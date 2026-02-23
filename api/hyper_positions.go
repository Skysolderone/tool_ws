package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

type HyperPositionItem struct {
	Coin           string  `json:"coin"`
	Side           string  `json:"side"` // LONG / SHORT
	Size           float64 `json:"size"`
	EntryPx        float64 `json:"entryPx"`
	MarkPx         float64 `json:"markPx"`
	PositionValue  float64 `json:"positionValue"`
	SignedNotional float64 `json:"signedNotional"`
	UnrealizedPnl  float64 `json:"unrealizedPnl"`
	LiquidationPx  float64 `json:"liquidationPx"`
	Leverage       float64 `json:"leverage"`
}

type HyperPositionsSnapshot struct {
	Address       string              `json:"address"`
	Time          int64               `json:"time"`
	AccountValue  float64             `json:"accountValue"`
	TotalNotional float64             `json:"totalNotional"` // abs(long)+abs(short)
	LongNotional  float64             `json:"longNotional"`
	ShortNotional float64             `json:"shortNotional"`
	NetExposure   float64             `json:"netExposure"` // signed sum
	Positions     []HyperPositionItem `json:"positions"`
}

// HandleGetHyperPositions GET /tool/hyper/positions?address=0x...
func HandleGetHyperPositions(c context.Context, ctx *app.RequestContext) {
	address := strings.TrimSpace(ctx.DefaultQuery("address", ""))
	if !reAddress.MatchString(address) {
		ctx.JSON(http.StatusBadRequest, utils.H{"error": "invalid address"})
		return
	}

	raw, err := fetchHyperInfo(map[string]any{
		"type": "clearinghouseState",
		"user": address,
	})
	if err != nil {
		ctx.JSON(http.StatusBadGateway, utils.H{"error": "fetch hyper position failed: " + err.Error()})
		return
	}

	snapshot, err := normalizeHyperPositionsSnapshot(address, raw)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": "normalize position failed: " + err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{"data": snapshot})
}

func normalizeHyperPositionsSnapshot(address string, raw any) (HyperPositionsSnapshot, error) {
	root, ok := raw.(map[string]any)
	if !ok {
		return HyperPositionsSnapshot{}, fmt.Errorf("unexpected response shape")
	}

	positionsRaw, _ := root["assetPositions"].([]any)
	items := make([]HyperPositionItem, 0, len(positionsRaw))
	longNotional := 0.0
	shortNotional := 0.0
	netExposure := 0.0

	for _, row := range positionsRaw {
		obj, ok := row.(map[string]any)
		if !ok {
			continue
		}

		pos := hpGetMap(obj, "position")
		if len(pos) == 0 {
			pos = obj
		}

		coin := hpPickString(pos["coin"])
		size := hpToFloat(pos["szi"])
		if size == 0 {
			continue
		}

		entryPx := hpToFloat(pos["entryPx"])
		markPx := hpToFloat(pos["markPx"])
		if markPx == 0 {
			markPx = hpToFloat(pos["markPrice"])
		}
		if markPx == 0 {
			markPx = hpToFloat(pos["px"])
		}

		positionValue := math.Abs(hpToFloat(pos["positionValue"]))
		if positionValue == 0 {
			refPx := markPx
			if refPx <= 0 {
				refPx = entryPx
			}
			positionValue = math.Abs(size) * refPx
		}

		signedNotional := positionValue
		side := "LONG"
		if size < 0 {
			side = "SHORT"
			signedNotional = -positionValue
		}

		unrealizedPnl := hpToFloat(pos["unrealizedPnl"])
		liqPx := hpToFloat(pos["liquidationPx"])
		leverage := hpToFloat(pos["leverage"])
		if leverage == 0 {
			leverage = hpToFloat(hpGetMap(pos, "leverage")["value"])
		}

		if signedNotional >= 0 {
			longNotional += positionValue
		} else {
			shortNotional += positionValue
		}
		netExposure += signedNotional

		items = append(items, HyperPositionItem{
			Coin:           strings.ToUpper(strings.TrimSpace(coin)),
			Side:           side,
			Size:           size,
			EntryPx:        entryPx,
			MarkPx:         markPx,
			PositionValue:  positionValue,
			SignedNotional: signedNotional,
			UnrealizedPnl:  unrealizedPnl,
			LiquidationPx:  liqPx,
			Leverage:       leverage,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return math.Abs(items[i].SignedNotional) > math.Abs(items[j].SignedNotional)
	})

	accountValue := hpToFloat(hpGetMap(root, "crossMarginSummary")["accountValue"])
	if accountValue == 0 {
		accountValue = hpToFloat(hpGetMap(root, "marginSummary")["accountValue"])
	}
	if accountValue == 0 {
		accountValue = hpToFloat(root["withdrawable"])
	}

	return HyperPositionsSnapshot{
		Address:       strings.ToLower(address),
		Time:          time.Now().UTC().UnixMilli(),
		AccountValue:  accountValue,
		TotalNotional: longNotional + shortNotional,
		LongNotional:  longNotional,
		ShortNotional: shortNotional,
		NetExposure:   netExposure,
		Positions:     items,
	}, nil
}

func hpGetMap(v map[string]any, key string) map[string]any {
	if v == nil {
		return nil
	}
	obj, _ := v[key].(map[string]any)
	return obj
}

func hpPickString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func hpToFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
}
