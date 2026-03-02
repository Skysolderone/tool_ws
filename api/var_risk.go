package api

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"gorm.io/gorm"
)

// VarRiskConfig VaR 风控配置
type VarRiskConfig struct {
	Enabled            bool    `json:"enabled"`
	RiskBudgetUSDT     float64 `json:"riskBudgetUSDT"`     // 风险预算上限
	BreachAction       string  `json:"breachAction"`        // "warn" / "lock"
	RefreshIntervalSec int     `json:"refreshIntervalSec"`  // 刷新间隔
	ConfidenceLevel    float64 `json:"confidenceLevel"`     // 0.95 or 0.99
}

// VarSnapshot VaR 快照（数据库模型）
type VarSnapshot struct {
	gorm.Model
	TotalVar95        float64 `gorm:"type:numeric(18,4)" json:"totalVar95"`
	TotalCVar95       float64 `gorm:"type:numeric(18,4)" json:"totalCVar95"`
	TotalVar99        float64 `gorm:"type:numeric(18,4)" json:"totalVar99"`
	RiskBudgetUsedPct float64 `gorm:"type:numeric(8,4)" json:"riskBudgetUsedPct"`
	Breached          bool    `json:"breached"`
	DetailJSON        string  `gorm:"type:text" json:"detailJson"`
}

type positionForVar struct {
	Symbol   string  `json:"symbol"`
	Notional float64 `json:"notional"`
	Side     string  `json:"side"` // LONG / SHORT
	VarPct   float64 `json:"varPct"`
}

var (
	varMu     sync.RWMutex
	varCfg    VarRiskConfig
	varLatest *VarSnapshot
	varStopCh chan struct{}
)

// InitVarRisk 初始化 VaR 风控
func InitVarRisk(cfg VarRiskConfig) {
	varMu.Lock()
	varCfg = cfg
	varMu.Unlock()

	if !cfg.Enabled {
		log.Println("[VarRisk] Disabled")
		return
	}
	if cfg.RefreshIntervalSec <= 0 {
		cfg.RefreshIntervalSec = 30
	}
	if cfg.ConfidenceLevel <= 0 {
		cfg.ConfidenceLevel = 0.95
	}

	varStopCh = make(chan struct{})
	go varRefreshLoop(cfg.RefreshIntervalSec)
	log.Printf("[VarRisk] Initialized: budget=%.0f, breach=%s, interval=%ds",
		cfg.RiskBudgetUSDT, cfg.BreachAction, cfg.RefreshIntervalSec)
}

func varRefreshLoop(intervalSec int) {
	// 启动后先等一会让持仓数据就绪
	time.Sleep(10 * time.Second)
	refreshVar()

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-varStopCh:
			return
		case <-ticker.C:
			refreshVar()
		}
	}
}

func refreshVar() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	positions, err := GetPositions(ctx)
	if err != nil {
		return
	}

	var posForVar []positionForVar
	var symbols []string
	symbolSet := map[string]bool{}

	for _, p := range positions {
		amt, _ := strconv.ParseFloat(p.PositionAmt, 64)
		if amt == 0 {
			continue
		}
		mark, _ := strconv.ParseFloat(p.MarkPrice, 64)
		notional := math.Abs(amt) * mark
		side := "LONG"
		if amt < 0 {
			side = "SHORT"
		}
		posForVar = append(posForVar, positionForVar{
			Symbol: p.Symbol, Notional: notional, Side: side,
		})
		if !symbolSet[p.Symbol] {
			symbols = append(symbols, p.Symbol)
			symbolSet[p.Symbol] = true
		}
	}

	if len(posForVar) == 0 {
		snap := &VarSnapshot{}
		varMu.Lock()
		varLatest = snap
		varMu.Unlock()
		return
	}

	snap := calcPortfolioVar(posForVar, symbols)

	varMu.Lock()
	budget := varCfg.RiskBudgetUSDT
	breachAction := varCfg.BreachAction
	varMu.Unlock()

	if budget > 0 {
		snap.RiskBudgetUsedPct = roundFloat(snap.TotalVar95/budget*100, 2)
		snap.Breached = snap.TotalVar95 > budget
	}

	varMu.Lock()
	varLatest = snap
	varMu.Unlock()

	// 超限处理
	if snap.Breached {
		RecordRiskTrigger("var_breach")
		if breachAction == "lock" {
			ActivateKillSwitch(KSAccount, "", "VaR 超出风险预算")
		}
		SendNotify("⚠️ VaR 超限警告\nVaR95: " + strconv.FormatFloat(snap.TotalVar95, 'f', 2, 64) + " USDT\n预算使用: " + strconv.FormatFloat(snap.RiskBudgetUsedPct, 'f', 1, 64) + "%")
	}

	// 持久化
	if DB != nil {
		DB.Create(snap)
	}
}

func calcPortfolioVar(positions []positionForVar, symbols []string) *VarSnapshot {
	// 获取相关性矩阵（如果可用）
	var corrMatrix [][]float64
	if len(symbols) > 1 {
		result, err := CalcCorrelation(symbols, "1h", 50)
		if err == nil && result != nil {
			corrMatrix = result.Matrix
		}
	}

	// 单个持仓的 VaR（假设日波动率约 3-5% 对加密货币）
	// 简化模型：单品种 VaR = notional * dailyVol * z_score
	z95 := 1.645
	z99 := 2.326
	dailyVolPct := 0.04 // 4% 默认日波动率

	var totalVar95Sq, totalVar99Sq float64
	details := make([]map[string]interface{}, 0, len(positions))

	for i, p := range positions {
		singleVar95 := p.Notional * dailyVolPct * z95
		singleVar99 := p.Notional * dailyVolPct * z99
		positions[i].VarPct = dailyVolPct * z95 * 100

		details = append(details, map[string]interface{}{
			"symbol": p.Symbol, "notional": p.Notional, "side": p.Side,
			"var95": roundFloat(singleVar95, 2), "var99": roundFloat(singleVar99, 2),
		})

		totalVar95Sq += singleVar95 * singleVar95
		totalVar99Sq += singleVar99 * singleVar99

		// 加入相关性交叉项
		if corrMatrix != nil {
			symIdx := -1
			for si, s := range symbols {
				if s == p.Symbol {
					symIdx = si
					break
				}
			}
			if symIdx >= 0 {
				for j, q := range positions {
					if j <= i {
						continue
					}
					qIdx := -1
					for si, s := range symbols {
						if s == q.Symbol {
							qIdx = si
							break
						}
					}
					if qIdx >= 0 && symIdx < len(corrMatrix) && qIdx < len(corrMatrix[symIdx]) {
						corr := corrMatrix[symIdx][qIdx]
						qVar95 := q.Notional * dailyVolPct * z95
						qVar99 := q.Notional * dailyVolPct * z99
						totalVar95Sq += 2 * corr * singleVar95 * qVar95
						totalVar99Sq += 2 * corr * singleVar99 * qVar99
					}
				}
			}
		}
	}

	totalVar95 := math.Sqrt(math.Max(totalVar95Sq, 0))
	totalVar99 := math.Sqrt(math.Max(totalVar99Sq, 0))
	// CVaR ≈ VaR * 1.25（简化）
	totalCVar95 := totalVar95 * 1.25

	detailBytes, _ := json.Marshal(details)

	return &VarSnapshot{
		TotalVar95:  roundFloat(totalVar95, 2),
		TotalCVar95: roundFloat(totalCVar95, 2),
		TotalVar99:  roundFloat(totalVar99, 2),
		DetailJSON:  string(detailBytes),
	}
}

// GetVarStatus 获取当前 VaR 状态
func GetVarStatus() *VarSnapshot {
	varMu.RLock()
	defer varMu.RUnlock()
	if varLatest == nil {
		return &VarSnapshot{}
	}
	clone := *varLatest
	return &clone
}

// HandleGetVarStatus GET /tool/risk/var
func HandleGetVarStatus(c context.Context, ctx *app.RequestContext) {
	ctx.JSON(http.StatusOK, utils.H{"data": GetVarStatus()})
}
