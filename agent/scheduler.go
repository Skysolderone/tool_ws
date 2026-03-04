package agent

import (
	"errors"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"tools/api"
)

var dailyAnalyzeSchedulerStarted atomic.Bool

// StartDailyAutoAnalyze 启动每天零点自动分析任务（本地时区）。
func StartDailyAutoAnalyze() {
	if chatModel == nil {
		log.Printf("[Agent] Daily auto analyze disabled: llm not configured")
		return
	}
	if !dailyAnalyzeSchedulerStarted.CompareAndSwap(false, true) {
		return
	}

	go runDailyAutoAnalyzeLoop()
}

func runDailyAutoAnalyzeLoop() {
	loc := time.Now().Location()
	for {
		now := time.Now().In(loc)
		next := nextMidnight(now)
		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		log.Printf("[Agent] Daily auto analyze scheduled at %s (in %v)", next.Format(time.RFC3339), wait.Round(time.Second))
		timer := time.NewTimer(wait)
		<-timer.C

		req := AnalysisRequest{
			Mode:    "full",
			Symbols: nil,
			Mock:    false,
			Async:   true,
			Execute: false,
		}
		reqID := "daily-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		taskID, err := enqueueAsyncAnalyze(reqID, req, api.AgentAnalysisSourceDailyAuto)
		if err != nil {
			log.Printf("[Agent][%s] daily auto analyze enqueue failed: %v", reqID, err)
			continue
		}
		log.Printf("[Agent][%s] daily auto analyze enqueued task_id=%d", reqID, taskID)
	}
}

func nextMidnight(now time.Time) time.Time {
	loc := now.Location()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func enqueueAsyncAnalyze(reqID string, req AnalysisRequest, source string) (uint, error) {
	record := &api.AgentAnalysisLog{
		Mode:        normalizeMode(req.Mode),
		Source:      source,
		Symbols:     strings.Join(req.Symbols, ","),
		Execute:     req.Execute,
		Status:      api.AgentAnalysisStatusPending,
		RequestBody: marshalToString(req),
	}
	if err := api.SaveAgentAnalysisLog(record); err != nil {
		return 0, err
	}
	if record.ID == 0 {
		return 0, errors.New("async analyze requires database logging")
	}

	log.Printf("[Agent][%s] async accepted task_id=%d", reqID, record.ID)
	go runAnalyzeAsyncTask(record.ID, reqID, req)
	return record.ID, nil
}
