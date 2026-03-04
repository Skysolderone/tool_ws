package agent

import (
	"reflect"
	"testing"
)

func TestExtractSymbolsFromChatMessage(t *testing.T) {
	got := extractSymbolsFromChatMessage("请评估 BTC、solusdt、LONG 和 NEWS 的影响")
	want := []string{"BTCUSDT", "SOLUSDT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractSymbolsFromChatMessage() = %v, want %v", got, want)
	}
}

func TestDetectChatIntent_DefaultFallback(t *testing.T) {
	intent := detectChatIntent(ChatRequest{
		Message: "BTC 接下来怎么看？",
	})

	wantSymbols := []string{"BTCUSDT"}
	if !reflect.DeepEqual(intent.Symbols, wantSymbols) {
		t.Fatalf("intent.Symbols = %v, want %v", intent.Symbols, wantSymbols)
	}
	if !intent.NeedPositions || !intent.NeedSignals || !intent.NeedSentiment || !intent.NeedBalance {
		t.Fatalf("fallback intent flags unexpected: %+v", intent)
	}
	if intent.NeedNews || intent.NeedJournal {
		t.Fatalf("fallback should not enable news/journal: %+v", intent)
	}
}

func TestDetectChatIntent_PositionsImpliesBalance(t *testing.T) {
	intent := detectChatIntent(ChatRequest{
		Message: "请看下我的持仓风险，BTC 需要减仓吗？",
		Symbols: []string{"ethusdt", "SOLUSDT"},
	})

	wantSymbols := []string{"ETHUSDT", "SOLUSDT", "BTCUSDT"}
	if !reflect.DeepEqual(intent.Symbols, wantSymbols) {
		t.Fatalf("intent.Symbols = %v, want %v", intent.Symbols, wantSymbols)
	}
	if !intent.NeedPositions {
		t.Fatalf("NeedPositions = false, want true")
	}
	if !intent.NeedBalance {
		t.Fatalf("NeedBalance = false, want true when positions requested")
	}
}

func TestTrimReply(t *testing.T) {
	got := trimReply(" 你好世界abc ", 4)
	want := "你好世界"
	if got != want {
		t.Fatalf("trimReply() = %q, want %q", got, want)
	}
}
