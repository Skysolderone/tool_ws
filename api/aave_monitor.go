package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

const (
	defaultAaveGraphQLEndpoint      = "https://api.v3.aave.com/graphql"
	defaultAaveMonitorIntervalSec   = 60
	defaultAaveMonitorMaxReserves   = 200
	defaultAaveUtilizationThreshold = 90.0
	defaultAaveBorrowAPYThreshold   = 20.0
	defaultAaveLPFeeRateThreshold   = 5.0
	defaultAaveHFThreshold          = 1.2
	defaultAaveAlertCooldownSec     = 600
	aaveMaxNotifyPerRound           = 8
)

// AaveWatchUser 需要监控健康度的用户配置。
type AaveWatchUser struct {
	Label                 string  `json:"label,omitempty"`
	ChainID               int     `json:"chainId"`
	Market                string  `json:"market"`
	User                  string  `json:"user"`
	HealthFactorThreshold float64 `json:"healthFactorThreshold,omitempty"`
}

// AaveMonitorConfig Aave 公共接口数据监控配置。
type AaveMonitorConfig struct {
	Endpoint                string          `json:"endpoint,omitempty"`
	ChainIDs                []int           `json:"chainIds,omitempty"`
	Symbols                 []string        `json:"symbols,omitempty"`
	IntervalSec             int             `json:"intervalSec,omitempty"`
	MaxReserves             int             `json:"maxReserves,omitempty"`
	LPFeeRateOnly           bool            `json:"lpFeeRateOnly,omitempty"`
	LPFeeRateThresholdPct   float64         `json:"lpFeeRateThresholdPct,omitempty"`
	UtilizationThresholdPct float64         `json:"utilizationThresholdPct,omitempty"`
	BorrowAPYThresholdPct   float64         `json:"borrowApyThresholdPct,omitempty"`
	HealthFactorThreshold   float64         `json:"healthFactorThreshold,omitempty"`
	AlertCooldownSec        int             `json:"alertCooldownSec,omitempty"`
	WatchUsers              []AaveWatchUser `json:"watchUsers,omitempty"`
}

// AaveReserveMetric 单个储备资产监控指标。
type AaveReserveMetric struct {
	ChainID               int     `json:"chainId"`
	ChainName             string  `json:"chainName"`
	MarketName            string  `json:"marketName"`
	Market                string  `json:"market"`
	Symbol                string  `json:"symbol"`
	Token                 string  `json:"token"`
	SupplyAPYPct          float64 `json:"supplyApyPct"`
	BorrowAPYPct          float64 `json:"borrowApyPct"`
	UtilizationPct        float64 `json:"utilizationPct"`
	AvailableLiquidityUSD float64 `json:"availableLiquidityUsd"`
	SupplyCapReached      bool    `json:"supplyCapReached"`
	BorrowCapReached      bool    `json:"borrowCapReached"`
	Frozen                bool    `json:"frozen"`
	Paused                bool    `json:"paused"`
	Borrowable            bool    `json:"borrowable"`
}

// AaveUserStateMetric 用户风控指标。
type AaveUserStateMetric struct {
	Label                 string   `json:"label"`
	ChainID               int      `json:"chainId"`
	Market                string   `json:"market"`
	User                  string   `json:"user"`
	HealthFactor          *float64 `json:"healthFactor,omitempty"`
	NetWorth              float64  `json:"netWorth"`
	TotalCollateralBase   float64  `json:"totalCollateralBase"`
	TotalDebtBase         float64  `json:"totalDebtBase"`
	AvailableBorrowsBase  float64  `json:"availableBorrowsBase"`
	EModeEnabled          bool     `json:"eModeEnabled"`
	IsolationMode         bool     `json:"isolationMode"`
	HealthFactorThreshold float64  `json:"healthFactorThreshold"`
}

// AaveMonitorAlert 告警事件。
type AaveMonitorAlert struct {
	Key       string  `json:"key"`
	Level     string  `json:"level"`
	Type      string  `json:"type"`
	Message   string  `json:"message"`
	ChainID   int     `json:"chainId,omitempty"`
	Market    string  `json:"market,omitempty"`
	Symbol    string  `json:"symbol,omitempty"`
	User      string  `json:"user,omitempty"`
	Value     float64 `json:"value,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
	Time      string  `json:"time"`
}

// AaveMonitorSnapshot 当前监控快照。
type AaveMonitorSnapshot struct {
	UpdateTime          string                `json:"updateTime"`
	Active              bool                  `json:"active"`
	Endpoint            string                `json:"endpoint"`
	Config              AaveMonitorConfig     `json:"config"`
	ReserveCount        int                   `json:"reserveCount"`
	Reserves            []AaveReserveMetric   `json:"reserves"`
	UserStates          []AaveUserStateMetric `json:"userStates,omitempty"`
	Alerts              []AaveMonitorAlert    `json:"alerts"`
	LastError           string                `json:"lastError,omitempty"`
	ConsecutiveErrors   int                   `json:"consecutiveErrors"`
	LastRoundDurationMs int64                 `json:"lastRoundDurationMs"`
}

type aaveMonitor struct {
	mu sync.RWMutex

	active    bool
	cancel    context.CancelFunc
	config    AaveMonitorConfig
	snapshot  AaveMonitorSnapshot
	client    *http.Client
	lastAlert map[string]time.Time
}

var aaveMon = &aaveMonitor{
	client:    &http.Client{Timeout: 15 * time.Second},
	lastAlert: make(map[string]time.Time),
}

type aaveGraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type aaveGraphQLError struct {
	Message string `json:"message"`
}

type aaveGraphQLResponse struct {
	Data   json.RawMessage    `json:"data"`
	Errors []aaveGraphQLError `json:"errors"`
}

type aaveMarketData struct {
	Markets []aaveMarket `json:"markets"`
}

type aaveMarket struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Chain   struct {
		ChainID int    `json:"chainId"`
		Name    string `json:"name"`
	} `json:"chain"`
	Reserves []aaveReserve `json:"reserves"`
}

type aaveReserve struct {
	UnderlyingToken struct {
		Symbol  string `json:"symbol"`
		Address string `json:"address"`
	} `json:"underlyingToken"`
	SupplyInfo struct {
		APY struct {
			Value string `json:"value"`
		} `json:"apy"`
		SupplyCapReached bool `json:"supplyCapReached"`
	} `json:"supplyInfo"`
	BorrowInfo *struct {
		APY struct {
			Value string `json:"value"`
		} `json:"apy"`
		UtilizationRate struct {
			Value string `json:"value"`
		} `json:"utilizationRate"`
		AvailableLiquidity struct {
			USD string `json:"usd"`
		} `json:"availableLiquidity"`
		BorrowCapReached bool `json:"borrowCapReached"`
	} `json:"borrowInfo"`
	IsFrozen bool `json:"isFrozen"`
	IsPaused bool `json:"isPaused"`
}

type aaveUserStateData struct {
	UserMarketState struct {
		HealthFactor         *string `json:"healthFactor"`
		NetWorth             string  `json:"netWorth"`
		TotalCollateralBase  string  `json:"totalCollateralBase"`
		TotalDebtBase        string  `json:"totalDebtBase"`
		AvailableBorrowsBase string  `json:"availableBorrowsBase"`
		EModeEnabled         bool    `json:"eModeEnabled"`
		IsInIsolationMode    bool    `json:"isInIsolationMode"`
	} `json:"userMarketState"`
}

const aaveMarketsQuery = `query($chainIds:[ChainId!]!){
  markets(request:{chainIds:$chainIds}){
    name
    address
    chain{ chainId name }
    reserves{
      underlyingToken{ symbol address }
      supplyInfo{
        apy{ value }
        supplyCapReached
      }
      borrowInfo{
        apy{ value }
        utilizationRate{ value }
        availableLiquidity{ usd }
        borrowCapReached
      }
      isFrozen
      isPaused
    }
  }
}`

const aaveUserStateQuery = `query($request:UserMarketStateRequest!){
  userMarketState(request:$request){
    healthFactor
    netWorth
    totalCollateralBase
    totalDebtBase
    availableBorrowsBase
    eModeEnabled
    isInIsolationMode
  }
}`

// StartAaveMonitor 启动 Aave 监控。
func StartAaveMonitor(config AaveMonitorConfig) error {
	cfg := normalizeAaveMonitorConfig(config)

	aaveMon.mu.Lock()
	defer aaveMon.mu.Unlock()

	if aaveMon.active && aaveMon.cancel != nil {
		aaveMon.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	aaveMon.active = true
	aaveMon.cancel = cancel
	aaveMon.config = cfg
	aaveMon.lastAlert = make(map[string]time.Time)
	aaveMon.snapshot = AaveMonitorSnapshot{
		UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		Active:     true,
		Endpoint:   cfg.Endpoint,
		Config:     cfg,
		Reserves:   make([]AaveReserveMetric, 0),
		Alerts:     make([]AaveMonitorAlert, 0),
	}

	go aaveMonitorLoop(ctx, cfg)

	log.Printf("[AaveMonitor] started endpoint=%s chains=%v symbols=%v interval=%ds",
		cfg.Endpoint, cfg.ChainIDs, cfg.Symbols, cfg.IntervalSec)
	return nil
}

// StopAaveMonitor 停止 Aave 监控。
func StopAaveMonitor() error {
	aaveMon.mu.Lock()
	defer aaveMon.mu.Unlock()

	if !aaveMon.active {
		return fmt.Errorf("aave monitor not active")
	}
	if aaveMon.cancel != nil {
		aaveMon.cancel()
	}
	aaveMon.active = false
	aaveMon.snapshot.Active = false
	log.Printf("[AaveMonitor] stopped")
	return nil
}

// GetAaveMonitorStatus 获取监控状态。
func GetAaveMonitorStatus() *AaveMonitorSnapshot {
	aaveMon.mu.RLock()
	defer aaveMon.mu.RUnlock()

	snap := cloneAaveSnapshot(aaveMon.snapshot)
	snap.Active = aaveMon.active
	snap.Config = aaveMon.config
	return &snap
}

func normalizeAaveMonitorConfig(cfg AaveMonitorConfig) AaveMonitorConfig {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultAaveGraphQLEndpoint
	}
	cfg.IntervalSec = fallbackInt(cfg.IntervalSec, defaultAaveMonitorIntervalSec)
	cfg.MaxReserves = fallbackInt(cfg.MaxReserves, defaultAaveMonitorMaxReserves)
	if cfg.UtilizationThresholdPct <= 0 {
		cfg.UtilizationThresholdPct = defaultAaveUtilizationThreshold
	}
	if cfg.BorrowAPYThresholdPct <= 0 {
		cfg.BorrowAPYThresholdPct = defaultAaveBorrowAPYThreshold
	}
	if cfg.LPFeeRateThresholdPct <= 0 {
		cfg.LPFeeRateThresholdPct = defaultAaveLPFeeRateThreshold
	}
	if cfg.HealthFactorThreshold <= 0 {
		cfg.HealthFactorThreshold = defaultAaveHFThreshold
	}
	cfg.AlertCooldownSec = fallbackInt(cfg.AlertCooldownSec, defaultAaveAlertCooldownSec)

	seenChains := make(map[int]struct{})
	chainIDs := make([]int, 0, len(cfg.ChainIDs))
	for _, chainID := range cfg.ChainIDs {
		if chainID <= 0 {
			continue
		}
		if _, exists := seenChains[chainID]; exists {
			continue
		}
		seenChains[chainID] = struct{}{}
		chainIDs = append(chainIDs, chainID)
	}
	if len(chainIDs) == 0 {
		chainIDs = []int{1}
	}
	cfg.ChainIDs = chainIDs

	symbols := make([]string, 0, len(cfg.Symbols))
	seenSymbols := make(map[string]struct{})
	for _, sym := range cfg.Symbols {
		norm := strings.ToUpper(strings.TrimSpace(sym))
		if norm == "" {
			continue
		}
		if _, exists := seenSymbols[norm]; exists {
			continue
		}
		seenSymbols[norm] = struct{}{}
		symbols = append(symbols, norm)
	}
	cfg.Symbols = symbols

	users := make([]AaveWatchUser, 0, len(cfg.WatchUsers))
	for _, user := range cfg.WatchUsers {
		user.Market = strings.TrimSpace(user.Market)
		user.User = strings.TrimSpace(user.User)
		if user.ChainID <= 0 || user.Market == "" || user.User == "" {
			continue
		}
		if user.HealthFactorThreshold <= 0 {
			user.HealthFactorThreshold = cfg.HealthFactorThreshold
		}
		if strings.TrimSpace(user.Label) == "" {
			user.Label = user.User
		}
		users = append(users, user)
	}
	cfg.WatchUsers = users
	return cfg
}

func aaveMonitorLoop(ctx context.Context, cfg AaveMonitorConfig) {
	ticker := time.NewTicker(time.Duration(cfg.IntervalSec) * time.Second)
	defer ticker.Stop()

	runAaveMonitorRound(ctx, cfg)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runAaveMonitorRound(ctx, cfg)
		}
	}
}

func runAaveMonitorRound(ctx context.Context, cfg AaveMonitorConfig) {
	start := time.Now()
	metrics, users, alerts, err := collectAaveMonitorData(ctx, cfg)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		alerts = append(alerts, AaveMonitorAlert{
			Key:     "aave_api_error",
			Level:   "critical",
			Type:    "api",
			Message: err.Error(),
			Time:    time.Now().Format("2006-01-02 15:04:05"),
		})
		aaveMon.mu.Lock()
		snap := aaveMon.snapshot
		snap.UpdateTime = time.Now().Format("2006-01-02 15:04:05")
		snap.Active = aaveMon.active
		snap.Endpoint = cfg.Endpoint
		snap.Config = cfg
		snap.LastError = err.Error()
		snap.ConsecutiveErrors++
		snap.Alerts = alerts
		snap.LastRoundDurationMs = duration
		aaveMon.snapshot = snap
		aaveMon.mu.Unlock()

		aaveMon.sendAlerts(alerts, cfg.AlertCooldownSec)
		log.Printf("[AaveMonitor] round failed: %v", err)
		return
	}

	aaveMon.mu.Lock()
	snap := aaveMon.snapshot
	snap.UpdateTime = time.Now().Format("2006-01-02 15:04:05")
	snap.Active = aaveMon.active
	snap.Endpoint = cfg.Endpoint
	snap.Config = cfg
	snap.ReserveCount = len(metrics)
	snap.Reserves = metrics
	snap.UserStates = users
	snap.Alerts = alerts
	snap.LastError = ""
	snap.ConsecutiveErrors = 0
	snap.LastRoundDurationMs = duration
	aaveMon.snapshot = snap
	aaveMon.mu.Unlock()

	aaveMon.sendAlerts(alerts, cfg.AlertCooldownSec)
	log.Printf("[AaveMonitor] round done: reserves=%d users=%d alerts=%d cost=%dms",
		len(metrics), len(users), len(alerts), duration)
}

func collectAaveMonitorData(ctx context.Context, cfg AaveMonitorConfig) ([]AaveReserveMetric, []AaveUserStateMetric, []AaveMonitorAlert, error) {
	markets, err := fetchAaveMarkets(ctx, cfg.Endpoint, cfg.ChainIDs)
	if err != nil {
		return nil, nil, nil, err
	}

	metrics := extractReserveMetrics(markets, cfg)
	alerts := evaluateReserveAlerts(metrics, cfg)

	users := make([]AaveUserStateMetric, 0, len(cfg.WatchUsers))
	for _, watch := range cfg.WatchUsers {
		state, stateErr := fetchAaveUserState(ctx, cfg.Endpoint, watch)
		if stateErr != nil {
			alerts = append(alerts, AaveMonitorAlert{
				Key:     fmt.Sprintf("user_fetch_error:%d:%s:%s", watch.ChainID, strings.ToLower(watch.Market), strings.ToLower(watch.User)),
				Level:   "critical",
				Type:    "user_api",
				Message: fmt.Sprintf("query user state failed label=%s err=%v", watch.Label, stateErr),
				ChainID: watch.ChainID,
				Market:  watch.Market,
				User:    watch.User,
				Time:    time.Now().Format("2006-01-02 15:04:05"),
			})
			continue
		}
		users = append(users, state)

		if state.HealthFactor != nil && *state.HealthFactor <= state.HealthFactorThreshold {
			alerts = append(alerts, AaveMonitorAlert{
				Key:       fmt.Sprintf("user_hf:%d:%s:%s", state.ChainID, strings.ToLower(state.Market), strings.ToLower(state.User)),
				Level:     "critical",
				Type:      "user_health_factor",
				Message:   fmt.Sprintf("health factor low label=%s hf=%.4f threshold=%.4f", state.Label, *state.HealthFactor, state.HealthFactorThreshold),
				ChainID:   state.ChainID,
				Market:    state.Market,
				User:      state.User,
				Value:     *state.HealthFactor,
				Threshold: state.HealthFactorThreshold,
				Time:      time.Now().Format("2006-01-02 15:04:05"),
			})
		}
	}

	sort.Slice(users, func(i, j int) bool {
		if users[i].ChainID != users[j].ChainID {
			return users[i].ChainID < users[j].ChainID
		}
		if users[i].Label != users[j].Label {
			return users[i].Label < users[j].Label
		}
		return users[i].User < users[j].User
	})

	return metrics, users, alerts, nil
}

func fetchAaveMarkets(ctx context.Context, endpoint string, chainIDs []int) ([]aaveMarket, error) {
	var data aaveMarketData
	if err := callAaveGraphQL(ctx, endpoint, aaveMarketsQuery, map[string]interface{}{
		"chainIds": chainIDs,
	}, &data); err != nil {
		return nil, err
	}
	return data.Markets, nil
}

func fetchAaveUserState(ctx context.Context, endpoint string, watch AaveWatchUser) (AaveUserStateMetric, error) {
	var data aaveUserStateData
	err := callAaveGraphQL(ctx, endpoint, aaveUserStateQuery, map[string]interface{}{
		"request": map[string]interface{}{
			"chainId": watch.ChainID,
			"market":  watch.Market,
			"user":    watch.User,
		},
	}, &data)
	if err != nil {
		return AaveUserStateMetric{}, err
	}

	return AaveUserStateMetric{
		Label:                 watch.Label,
		ChainID:               watch.ChainID,
		Market:                watch.Market,
		User:                  watch.User,
		HealthFactor:          parseOptionalNumber(data.UserMarketState.HealthFactor),
		NetWorth:              parseNumeric(data.UserMarketState.NetWorth),
		TotalCollateralBase:   parseNumeric(data.UserMarketState.TotalCollateralBase),
		TotalDebtBase:         parseNumeric(data.UserMarketState.TotalDebtBase),
		AvailableBorrowsBase:  parseNumeric(data.UserMarketState.AvailableBorrowsBase),
		EModeEnabled:          data.UserMarketState.EModeEnabled,
		IsolationMode:         data.UserMarketState.IsInIsolationMode,
		HealthFactorThreshold: watch.HealthFactorThreshold,
	}, nil
}

func callAaveGraphQL(ctx context.Context, endpoint string, query string, variables map[string]interface{}, out interface{}) error {
	reqBody := aaveGraphQLRequest{
		Query:     query,
		Variables: variables,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal graphql body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := aaveMon.client.Do(req)
	if err != nil {
		return fmt.Errorf("request graphql: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read graphql response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("graphql status=%d body=%s", resp.StatusCode, msg)
	}

	var payload aaveGraphQLResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	if len(payload.Errors) > 0 {
		msgs := make([]string, 0, len(payload.Errors))
		for i, gqlErr := range payload.Errors {
			if i >= 3 {
				break
			}
			msgs = append(msgs, strings.TrimSpace(gqlErr.Message))
		}
		return fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}
	if len(payload.Data) == 0 {
		return fmt.Errorf("empty graphql data")
	}
	if err := json.Unmarshal(payload.Data, out); err != nil {
		return fmt.Errorf("decode graphql data: %w", err)
	}
	return nil
}

func extractReserveMetrics(markets []aaveMarket, cfg AaveMonitorConfig) []AaveReserveMetric {
	symbolSet := make(map[string]struct{}, len(cfg.Symbols))
	for _, symbol := range cfg.Symbols {
		symbolSet[strings.ToUpper(symbol)] = struct{}{}
	}

	metrics := make([]AaveReserveMetric, 0, 128)
	for _, market := range markets {
		for _, reserve := range market.Reserves {
			symbol := strings.ToUpper(strings.TrimSpace(reserve.UnderlyingToken.Symbol))
			if len(symbolSet) > 0 {
				if _, ok := symbolSet[symbol]; !ok {
					continue
				}
			}

			m := AaveReserveMetric{
				ChainID:          market.Chain.ChainID,
				ChainName:        market.Chain.Name,
				MarketName:       market.Name,
				Market:           market.Address,
				Symbol:           symbol,
				Token:            reserve.UnderlyingToken.Address,
				SupplyAPYPct:     parseNumeric(reserve.SupplyInfo.APY.Value) * 100,
				SupplyCapReached: reserve.SupplyInfo.SupplyCapReached,
				Frozen:           reserve.IsFrozen,
				Paused:           reserve.IsPaused,
				Borrowable:       reserve.BorrowInfo != nil,
			}
			if reserve.BorrowInfo != nil {
				m.BorrowAPYPct = parseNumeric(reserve.BorrowInfo.APY.Value) * 100
				m.UtilizationPct = parseNumeric(reserve.BorrowInfo.UtilizationRate.Value) * 100
				m.AvailableLiquidityUSD = parseNumeric(reserve.BorrowInfo.AvailableLiquidity.USD)
				m.BorrowCapReached = reserve.BorrowInfo.BorrowCapReached
			}
			metrics = append(metrics, m)
		}
	}

	sort.Slice(metrics, func(i, j int) bool {
		if cfg.LPFeeRateOnly {
			if metrics[i].SupplyAPYPct != metrics[j].SupplyAPYPct {
				return metrics[i].SupplyAPYPct > metrics[j].SupplyAPYPct
			}
			if metrics[i].ChainID != metrics[j].ChainID {
				return metrics[i].ChainID < metrics[j].ChainID
			}
			if metrics[i].Market != metrics[j].Market {
				return metrics[i].Market < metrics[j].Market
			}
			return metrics[i].Symbol < metrics[j].Symbol
		}

		if metrics[i].Paused != metrics[j].Paused {
			return metrics[i].Paused
		}
		if metrics[i].Frozen != metrics[j].Frozen {
			return metrics[i].Frozen
		}
		if metrics[i].UtilizationPct != metrics[j].UtilizationPct {
			return metrics[i].UtilizationPct > metrics[j].UtilizationPct
		}
		if metrics[i].BorrowAPYPct != metrics[j].BorrowAPYPct {
			return metrics[i].BorrowAPYPct > metrics[j].BorrowAPYPct
		}
		if metrics[i].ChainID != metrics[j].ChainID {
			return metrics[i].ChainID < metrics[j].ChainID
		}
		if metrics[i].Market != metrics[j].Market {
			return metrics[i].Market < metrics[j].Market
		}
		return metrics[i].Symbol < metrics[j].Symbol
	})

	if cfg.MaxReserves > 0 && len(metrics) > cfg.MaxReserves {
		metrics = metrics[:cfg.MaxReserves]
	}
	return metrics
}

func evaluateReserveAlerts(metrics []AaveReserveMetric, cfg AaveMonitorConfig) []AaveMonitorAlert {
	now := time.Now().Format("2006-01-02 15:04:05")
	alerts := make([]AaveMonitorAlert, 0)

	for _, m := range metrics {
		if cfg.LPFeeRateOnly {
			if m.SupplyAPYPct >= cfg.LPFeeRateThresholdPct {
				alerts = append(alerts, AaveMonitorAlert{
					Key:       fmt.Sprintf("lp_fee_rate:%d:%s:%s", m.ChainID, strings.ToLower(m.Market), strings.ToLower(m.Symbol)),
					Level:     "warning",
					Type:      "high_lp_fee_rate",
					Message:   fmt.Sprintf("%s/%s LP fee rate %.2f%% >= %.2f%%", m.MarketName, m.Symbol, m.SupplyAPYPct, cfg.LPFeeRateThresholdPct),
					ChainID:   m.ChainID,
					Market:    m.Market,
					Symbol:    m.Symbol,
					Value:     m.SupplyAPYPct,
					Threshold: cfg.LPFeeRateThresholdPct,
					Time:      now,
				})
			}
			continue
		}

		if m.Paused || m.Frozen {
			alerts = append(alerts, AaveMonitorAlert{
				Key:     fmt.Sprintf("reserve_state:%d:%s:%s", m.ChainID, strings.ToLower(m.Market), strings.ToLower(m.Symbol)),
				Level:   "critical",
				Type:    "reserve_state",
				Message: fmt.Sprintf("%s/%s paused=%v frozen=%v", m.MarketName, m.Symbol, m.Paused, m.Frozen),
				ChainID: m.ChainID,
				Market:  m.Market,
				Symbol:  m.Symbol,
				Time:    now,
			})
		}

		if m.SupplyCapReached {
			alerts = append(alerts, AaveMonitorAlert{
				Key:     fmt.Sprintf("supply_cap:%d:%s:%s", m.ChainID, strings.ToLower(m.Market), strings.ToLower(m.Symbol)),
				Level:   "warning",
				Type:    "supply_cap_reached",
				Message: fmt.Sprintf("%s/%s supply cap reached", m.MarketName, m.Symbol),
				ChainID: m.ChainID,
				Market:  m.Market,
				Symbol:  m.Symbol,
				Time:    now,
			})
		}

		if m.BorrowCapReached {
			alerts = append(alerts, AaveMonitorAlert{
				Key:     fmt.Sprintf("borrow_cap:%d:%s:%s", m.ChainID, strings.ToLower(m.Market), strings.ToLower(m.Symbol)),
				Level:   "warning",
				Type:    "borrow_cap_reached",
				Message: fmt.Sprintf("%s/%s borrow cap reached", m.MarketName, m.Symbol),
				ChainID: m.ChainID,
				Market:  m.Market,
				Symbol:  m.Symbol,
				Time:    now,
			})
		}

		if m.Borrowable && m.UtilizationPct >= cfg.UtilizationThresholdPct {
			alerts = append(alerts, AaveMonitorAlert{
				Key:       fmt.Sprintf("utilization:%d:%s:%s", m.ChainID, strings.ToLower(m.Market), strings.ToLower(m.Symbol)),
				Level:     "warning",
				Type:      "high_utilization",
				Message:   fmt.Sprintf("%s/%s utilization %.2f%% >= %.2f%%", m.MarketName, m.Symbol, m.UtilizationPct, cfg.UtilizationThresholdPct),
				ChainID:   m.ChainID,
				Market:    m.Market,
				Symbol:    m.Symbol,
				Value:     m.UtilizationPct,
				Threshold: cfg.UtilizationThresholdPct,
				Time:      now,
			})
		}

		if m.Borrowable && m.BorrowAPYPct >= cfg.BorrowAPYThresholdPct {
			alerts = append(alerts, AaveMonitorAlert{
				Key:       fmt.Sprintf("borrow_apy:%d:%s:%s", m.ChainID, strings.ToLower(m.Market), strings.ToLower(m.Symbol)),
				Level:     "warning",
				Type:      "high_borrow_apy",
				Message:   fmt.Sprintf("%s/%s borrow APY %.2f%% >= %.2f%%", m.MarketName, m.Symbol, m.BorrowAPYPct, cfg.BorrowAPYThresholdPct),
				ChainID:   m.ChainID,
				Market:    m.Market,
				Symbol:    m.Symbol,
				Value:     m.BorrowAPYPct,
				Threshold: cfg.BorrowAPYThresholdPct,
				Time:      now,
			})
		}
	}

	return alerts
}

func (m *aaveMonitor) sendAlerts(alerts []AaveMonitorAlert, cooldownSec int) {
	if len(alerts) == 0 {
		return
	}

	cooldown := time.Duration(cooldownSec) * time.Second
	if cooldown <= 0 {
		cooldown = time.Duration(defaultAaveAlertCooldownSec) * time.Second
	}

	now := time.Now()
	sent := 0
	for _, alert := range alerts {
		if sent >= aaveMaxNotifyPerRound {
			break
		}
		if !m.shouldSendAlert(alert.Key, now, cooldown) {
			continue
		}
		SendNotify(fmt.Sprintf("*Aave监控告警* [%s/%s]\n%s", alert.Level, alert.Type, alert.Message))
		sent++
	}
}

func (m *aaveMonitor) shouldSendAlert(key string, now time.Time, cooldown time.Duration) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	last, ok := m.lastAlert[key]
	if ok && now.Sub(last) < cooldown {
		return false
	}
	m.lastAlert[key] = now
	return true
}

func parseOptionalNumber(v *string) *float64 {
	if v == nil {
		return nil
	}
	n := parseNumeric(*v)
	return &n
}

func cloneAaveSnapshot(src AaveMonitorSnapshot) AaveMonitorSnapshot {
	dst := src
	if src.Reserves != nil {
		dst.Reserves = append([]AaveReserveMetric(nil), src.Reserves...)
	}
	if src.UserStates != nil {
		dst.UserStates = append([]AaveUserStateMetric(nil), src.UserStates...)
	}
	if src.Alerts != nil {
		dst.Alerts = append([]AaveMonitorAlert(nil), src.Alerts...)
	}
	return dst
}

func fallbackInt(v int, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

// HandleStartAaveMonitor POST /tool/aave/monitor/start
func HandleStartAaveMonitor(c context.Context, ctx *app.RequestContext) {
	var req AaveMonitorConfig
	if err := ctx.BindAndValidate(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := StartAaveMonitor(req); err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{
		"message": "aave monitor started",
		"data":    GetAaveMonitorStatus(),
	})
}

// HandleStopAaveMonitor POST /tool/aave/monitor/stop
func HandleStopAaveMonitor(c context.Context, ctx *app.RequestContext) {
	if err := StopAaveMonitor(); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"message": "aave monitor stopped"})
}

// HandleAaveMonitorStatus GET /tool/aave/monitor/status
func HandleAaveMonitorStatus(c context.Context, ctx *app.RequestContext) {
	ctx.JSON(http.StatusOK, utils.H{"data": GetAaveMonitorStatus()})
}
