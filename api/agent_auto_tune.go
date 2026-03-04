package api

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	autoTuneLowHitRateThreshold  = 40.0
	autoTuneRecoverThreshold     = 60.0
	autoTuneLowHitStreakDays     = 3
	autoTuneWorstSymbolStreakDay = 7
)

var autoTuneState struct {
	mu              sync.Mutex
	originalProfile string
}

// AgentTuneLog 自动调参记录。
type AgentTuneLog struct {
	gorm.Model
	TuneDate       time.Time `gorm:"index" json:"tuneDate"`
	PrevProfile    string    `gorm:"type:varchar(20)" json:"prevProfile"`
	NewProfile     string    `gorm:"type:varchar(20)" json:"newProfile"`
	HitRate24H     float64   `gorm:"type:numeric(8,4)" json:"hitRate24H"`
	WorstSymbols   string    `gorm:"type:text" json:"worstSymbols"`
	AddedSymbols   string    `gorm:"type:text" json:"addedSymbols"`
	RemovedSymbols string    `gorm:"type:text" json:"removedSymbols"`
	Reason         string    `gorm:"type:text" json:"reason"`
}

// SaveAgentTuneLog 保存自动调参日志。
func SaveAgentTuneLog(record *AgentTuneLog) error {
	if DB == nil || record == nil {
		return nil
	}
	if record.TuneDate.IsZero() {
		record.TuneDate = time.Now()
	}
	return DB.Create(record).Error
}

// AutoTuneAgentPolicy 根据评估摘要自动调整 Agent 执行策略。
// 仅修改内存中的 Cfg.Agent，不会写回 config.json。
func AutoTuneAgentPolicy(summary AgentEvalSummary) {
	if DB == nil {
		return
	}

	currentProfile := normalizeProfile(strings.TrimSpace(Cfg.Agent.ExecutionProfile))
	if currentProfile == "" {
		currentProfile = "custom"
	}
	ensureOriginalProfile(currentProfile)

	lowStreak := has24HHitRateStreak(autoTuneLowHitStreakDays, func(rate float64) bool {
		return rate < autoTuneLowHitRateThreshold
	})
	recoverStreak := has24HHitRateStreak(autoTuneLowHitStreakDays, func(rate float64) bool {
		return rate > autoTuneRecoverThreshold
	})
	poorSymbols := findPoorSymbolsStreak(autoTuneWorstSymbolStreakDay, autoTuneLowHitRateThreshold)

	newProfile := currentProfile
	reasons := make([]string, 0, 2)
	if lowStreak && currentProfile != "conservative" {
		newProfile = "conservative"
		reasons = append(reasons, "24h hit rate < 40% for 3 consecutive days")
	}
	if recoverStreak && currentProfile == "conservative" {
		restored := getOriginalProfile()
		if restored == "" {
			restored = "custom"
		}
		newProfile = restored
		reasons = append(reasons, "24h hit rate > 60% for 3 consecutive days")
	}

	beforeBlocked := normalizeSymbols(Cfg.Agent.BlockedSymbols)
	afterBlocked, added, removed := reconcileBlockedSymbols(beforeBlocked, poorSymbols, recoverStreak)

	changedProfile := newProfile != currentProfile
	changedBlocked := len(added) > 0 || len(removed) > 0
	if !changedProfile && !changedBlocked {
		return
	}

	Cfg.Agent.ExecutionProfile = newProfile
	Cfg.Agent.BlockedSymbols = afterBlocked

	reasonText := strings.Join(reasons, "; ")
	if reasonText == "" && changedBlocked {
		reasonText = "worst symbols underperformed for 7 consecutive days"
	}
	log.Printf("[AgentTune] applied profile=%s->%s added=%v removed=%v reason=%s", currentProfile, newProfile, added, removed, reasonText)

	_ = SaveAgentTuneLog(&AgentTuneLog{
		TuneDate:       time.Now(),
		PrevProfile:    currentProfile,
		NewProfile:     newProfile,
		HitRate24H:     summary.HitRate24H,
		WorstSymbols:   strings.Join(summary.WorstSymbols, ","),
		AddedSymbols:   strings.Join(added, ","),
		RemovedSymbols: strings.Join(removed, ","),
		Reason:         reasonText,
	})
}

func normalizeProfile(profile string) string {
	p := strings.ToLower(strings.TrimSpace(profile))
	switch p {
	case "conservative", "aggressive", "custom":
		return p
	default:
		return "custom"
	}
}

func ensureOriginalProfile(currentProfile string) {
	autoTuneState.mu.Lock()
	defer autoTuneState.mu.Unlock()
	if autoTuneState.originalProfile == "" && currentProfile != "conservative" {
		autoTuneState.originalProfile = currentProfile
	}
	if autoTuneState.originalProfile == "" {
		autoTuneState.originalProfile = "custom"
	}
}

func getOriginalProfile() string {
	autoTuneState.mu.Lock()
	defer autoTuneState.mu.Unlock()
	if autoTuneState.originalProfile == "" {
		autoTuneState.originalProfile = "custom"
	}
	return autoTuneState.originalProfile
}

func reconcileBlockedSymbols(before []string, poor []string, recovery bool) (after, added, removed []string) {
	beforeSet := make(map[string]struct{}, len(before))
	for _, symbol := range normalizeSymbols(before) {
		beforeSet[symbol] = struct{}{}
	}

	afterSet := make(map[string]struct{}, len(beforeSet))
	if recovery {
		for _, symbol := range normalizeSymbols(poor) {
			afterSet[symbol] = struct{}{}
		}
	} else {
		for symbol := range beforeSet {
			afterSet[symbol] = struct{}{}
		}
		for _, symbol := range normalizeSymbols(poor) {
			afterSet[symbol] = struct{}{}
		}
	}

	for symbol := range afterSet {
		if _, ok := beforeSet[symbol]; !ok {
			added = append(added, symbol)
		}
	}
	for symbol := range beforeSet {
		if _, ok := afterSet[symbol]; !ok {
			removed = append(removed, symbol)
		}
	}

	after = setToSortedSlice(afterSet)
	sort.Strings(added)
	sort.Strings(removed)
	return after, added, removed
}

func has24HHitRateStreak(days int, predicate func(rate float64) bool) bool {
	if DB == nil || days <= 0 {
		return false
	}
	rates := buildDaily24HHitRates(days + 2)
	now := time.Now()
	for i := 1; i <= days; i++ {
		dayKey := now.AddDate(0, 0, -i).Format("2006-01-02")
		stat, ok := rates[dayKey]
		if !ok || stat.total == 0 {
			return false
		}
		rate := float64(stat.hits) / float64(stat.total) * 100
		if !predicate(rate) {
			return false
		}
	}
	return true
}

func findPoorSymbolsStreak(days int, threshold float64) []string {
	if DB == nil || days <= 0 {
		return nil
	}

	now := time.Now()
	since := now.AddDate(0, 0, -(days + 2))
	var records []AgentEvaluationRecord
	if err := DB.Where("evaluated_24h = ? AND created_at >= ?", true, since).
		Order("created_at DESC").
		Limit(2000).
		Find(&records).Error; err != nil {
		return nil
	}
	if len(records) == 0 {
		return nil
	}

	type ratioStat struct {
		total int
		hits  int
	}
	daySymbolStats := make(map[string]map[string]*ratioStat)
	for _, record := range records {
		day := record.CreatedAt.In(time.Local).Format("2006-01-02")
		symbol := strings.ToUpper(strings.TrimSpace(record.Symbol))
		if symbol == "" {
			continue
		}
		if daySymbolStats[day] == nil {
			daySymbolStats[day] = make(map[string]*ratioStat)
		}
		if daySymbolStats[day][symbol] == nil {
			daySymbolStats[day][symbol] = &ratioStat{}
		}
		stat := daySymbolStats[day][symbol]
		stat.total++
		if record.PnlPct24H > 0 {
			stat.hits++
		}
	}

	intersection := map[string]struct{}{}
	for i := 1; i <= days; i++ {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		symbolStats, ok := daySymbolStats[day]
		if !ok || len(symbolStats) == 0 {
			return nil
		}

		dayPoor := make(map[string]struct{})
		for symbol, stat := range symbolStats {
			if stat.total == 0 {
				continue
			}
			rate := float64(stat.hits) / float64(stat.total) * 100
			if rate < threshold {
				dayPoor[symbol] = struct{}{}
			}
		}
		if len(dayPoor) == 0 {
			return nil
		}

		if i == 1 {
			intersection = dayPoor
			continue
		}
		for symbol := range intersection {
			if _, ok := dayPoor[symbol]; !ok {
				delete(intersection, symbol)
			}
		}
		if len(intersection) == 0 {
			return nil
		}
	}

	return setToSortedSlice(intersection)
}

func buildDaily24HHitRates(days int) map[string]struct{ total, hits int } {
	out := make(map[string]struct{ total, hits int })
	if DB == nil || days <= 0 {
		return out
	}

	since := time.Now().AddDate(0, 0, -days)
	var records []AgentEvaluationRecord
	if err := DB.Where("evaluated_24h = ? AND created_at >= ?", true, since).
		Order("created_at DESC").
		Limit(2000).
		Find(&records).Error; err != nil {
		return out
	}
	for _, record := range records {
		day := record.CreatedAt.In(time.Local).Format("2006-01-02")
		stat := out[day]
		stat.total++
		if record.PnlPct24H > 0 {
			stat.hits++
		}
		out[day] = stat
	}
	return out
}

func setToSortedSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for symbol := range set {
		out = append(out, symbol)
	}
	sort.Strings(out)
	return out
}
