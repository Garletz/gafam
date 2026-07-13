package main

import (
	"net/http"

	"github.com/Garletz/gafam/vpc-relay/suparna"
)

func suparnaStatusHandler(w http.ResponseWriter, r *http.Request) {
	suparna.StatusHandler(w, r)
}

func suparnaReadingHandler(w http.ResponseWriter, r *http.Request) {
	suparna.ReadingHandler(w, r)
}

func suparnaReadLogsHandler(w http.ResponseWriter, r *http.Request) {
	suparna.ReadDayHandler(w, r,
		func(day string, offset, limit int) ([]suparna.LogLine, int, error) {
			entries, total, err := readLogDay(day, offset, limit)
			if err != nil {
				return nil, 0, err
			}
			lines := make([]suparna.LogLine, len(entries))
			for i, e := range entries {
				lines[i] = suparna.LogLine{
					Ts: e.Ts, Source: e.Source, Level: e.Level, Tag: e.Tag, Message: e.Message,
				}
			}
			return lines, int(total), nil
		},
		func() bool {
			scrcpyHub.mu.RLock()
			defer scrcpyHub.mu.RUnlock()
			// Block only active browser relay streams (RAM on 1 Go VPS).
			// Manager bridge alone (no cloud viewers) must not block Suparna.
			return len(scrcpyHub.viewers) > 0 || len(scrcpyHub.shellViewers) > 0
		},
	)
}
