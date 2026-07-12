package main

import (
	"net/http"

	"github.com/Garletz/gafam/vpc-relay/suparna"
)

func suparnaStatusHandler(w http.ResponseWriter, r *http.Request) {
	suparna.StatusHandler(w, r)
}

func suparnaDownloadModelHandler(w http.ResponseWriter, r *http.Request) {
	suparna.DownloadModelHandler(w, r)
}

func suparnaReadLogsHandler(w http.ResponseWriter, r *http.Request) {
	suparna.ReadDayHandler(w, r, func(day string, offset, limit int) ([]suparna.LogLine, int, error) {
		entries, _, err := readLogDay(day, offset, limit)
		if err != nil {
			return nil, 0, err
		}
		lines := make([]suparna.LogLine, len(entries))
		for i, e := range entries {
			lines[i] = suparna.LogLine{
				Ts: e.Ts, Source: e.Source, Level: e.Level, Tag: e.Tag, Message: e.Message,
			}
		}
		return lines, len(lines), nil
	})
}

func suparnaScanSmsHandler(w http.ResponseWriter, r *http.Request) {
	suparna.ScanSmsHandler(w, r, queryRecentSmsPlain)
}

func queryRecentSmsPlain(limit int) ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT id, sender, body, timestamp, status FROM gafam_sms ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int
		var sender, body, status string
		var timestamp int64
		if err := rows.Scan(&id, &sender, &body, &timestamp, &status); err != nil {
			continue
		}
		direction := "inbound"
		if status == "outbound" || status == "sent" {
			direction = "outbound"
		}
		item := map[string]interface{}{
			"id": id, "sender": sender, "body": body,
			"timestamp": timestamp, "status": status, "direction": direction,
		}
		if direction == "inbound" {
			if codes := suparna.DetectCodes(body); len(codes) > 0 {
				item["codes"] = codes
			}
		}
		list = append(list, item)
	}
	return list, nil
}
