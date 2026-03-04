package api

import "strings"

var defaultAgentAllowedActions = []string{"open", "add", "close", "reduce", "set_sl", "set_tp"}

// AgentExecutionPolicy Agent 执行策略（模板解析后的生效配置）。
type AgentExecutionPolicy struct {
	Profile              string   `json:"profile"`                 // conservative|aggressive|custom
	EnableExecution      bool     `json:"enable_execution"`        // 是否允许执行动作
	MaxActionsPerRequest int      `json:"max_actions_per_request"` // 单次最多执行多少条建议
	AllowedActions       []string `json:"allowed_actions"`         // 允许动作白名单（小写）
	AllowedSymbols       []string `json:"allowed_symbols"`         // 允许交易对白名单（大写）
	BlockedSymbols       []string `json:"blocked_symbols"`         // 禁止交易对黑名单（大写）
	Description          string   `json:"description"`             // 策略说明
}

// ResolveAgentExecutionPolicy 解析 execution_profile，返回最终生效策略。
func ResolveAgentExecutionPolicy() AgentExecutionPolicy {
	cfg := Cfg.Agent
	profile := strings.ToLower(strings.TrimSpace(cfg.ExecutionProfile))
	if profile == "" {
		profile = "custom"
	}

	p := AgentExecutionPolicy{
		Profile:         profile,
		AllowedSymbols:  normalizeSymbols(cfg.AllowedSymbols),
		BlockedSymbols:  normalizeSymbols(cfg.BlockedSymbols),
		AllowedActions:  nil,
		Description:     "",
		EnableExecution: false,
	}

	switch profile {
	case "conservative":
		p.EnableExecution = false
		p.MaxActionsPerRequest = 2
		p.AllowedActions = []string{"close", "reduce", "set_sl", "set_tp"}
		p.Description = "保守模板：默认仅分析不执行，限制单次动作数并仅保留风险收敛类动作。"
	case "aggressive":
		p.EnableExecution = true
		p.MaxActionsPerRequest = 10
		p.AllowedActions = append([]string{}, defaultAgentAllowedActions...)
		p.Description = "激进模板：允许执行并放宽单次动作数量，支持完整动作集。"
	default:
		p.Profile = "custom"
		p.EnableExecution = cfg.EnableExecution
		p.MaxActionsPerRequest = cfg.MaxActionsPerRequest
		p.AllowedActions = append([]string{}, cfg.AllowedActions...)
		p.Description = "自定义模板：完全使用配置文件中的执行开关、动作白名单和数量限制。"
	}

	if p.MaxActionsPerRequest <= 0 {
		p.MaxActionsPerRequest = 5
	}
	p.AllowedActions = normalizeActions(p.AllowedActions)
	if len(p.AllowedActions) == 0 {
		p.AllowedActions = append([]string{}, defaultAgentAllowedActions...)
	}

	return p
}

func normalizeActions(actions []string) []string {
	if len(actions) == 0 {
		return nil
	}
	out := make([]string, 0, len(actions))
	seen := make(map[string]struct{})
	for _, action := range actions {
		a := strings.ToLower(strings.TrimSpace(action))
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

func normalizeSymbols(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]string, 0, len(symbols))
	seen := make(map[string]struct{})
	for _, symbol := range symbols {
		s := strings.ToUpper(strings.TrimSpace(symbol))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
