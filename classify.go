package worktime

import (
	"sort"
	"time"
)

const (
	dayRegularCap  = 8 * time.Hour
	dayOTCap       = 12 * time.Hour
	weekRegularCap = 40 * time.Hour
)

// classifyDay 对同一归班日期的有效区间按时间先后分类：
// 累计超过 8 小时记加班，超过 12 小时记双倍；8 小时规则可配置关闭。
func classifyDay(ivs []interval, dailyOT bool) []Segment {
	sorted := make([]interval, len(ivs))
	copy(sorted, ivs)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].start.Equal(sorted[j].start) {
			return sorted[i].start.Before(sorted[j].start)
		}
		return sorted[i].end.Before(sorted[j].end)
	})

	var segs []Segment
	var elapsed time.Duration
	for _, iv := range sorted {
		for cursor := iv.start; cursor.Before(iv.end); {
			cat, limit := dayCategory(elapsed, dailyOT)
			next := iv.end
			if boundary := cursor.Add(limit - elapsed); boundary.Before(next) {
				next = boundary
			}
			segs = append(segs, Segment{
				Start:     cursor,
				End:       next,
				Category:  cat,
				RecordIDs: recordIDs(iv),
			})
			elapsed += next.Sub(cursor)
			cursor = next
		}
	}
	return segs
}

// dayCategory 返回累计 elapsed 分钟处当日工时的类别与该类别的累计上限。
func dayCategory(elapsed time.Duration, dailyOT bool) (Category, time.Duration) {
	if dailyOT && elapsed < dayRegularCap {
		return Regular, dayRegularCap
	}
	if elapsed < dayOTCap {
		if dailyOT {
			return Overtime, dayOTCap
		}
		return Regular, dayOTCap
	}
	return Double, time.Duration(1) << 62 // 双倍无上限
}

// applyWeekly 把已分类段按周（周日 0 点起）切开，
// 一周内正常工时累计超 40 小时的部分改记加班，按周汇总。
func applyWeekly(segs []Segment) []WeekResult {
	byWeek := map[time.Time][]Segment{}
	var order []time.Time
	for _, s := range segs {
		for _, piece := range splitByWeek(s) {
			ws := weekStart(piece.Start)
			if _, ok := byWeek[ws]; !ok {
				order = append(order, ws)
			}
			byWeek[ws] = append(byWeek[ws], piece)
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i].Before(order[j]) })

	weeks := make([]WeekResult, 0, len(order))
	for _, ws := range order {
		weeks = append(weeks, classifyWeek(ws, byWeek[ws]))
	}
	return weeks
}

// classifyWeek 对一周内的段按时间先后应用 40 小时规则并汇总。
func classifyWeek(ws time.Time, segs []Segment) WeekResult {
	sorted := make([]Segment, len(segs))
	copy(sorted, segs)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].Start.Equal(sorted[j].Start) {
			return sorted[i].Start.Before(sorted[j].Start)
		}
		return sorted[i].End.Before(sorted[j].End)
	})

	res := WeekResult{WeekStart: ws}
	var regularElapsed time.Duration
	for _, s := range sorted {
		for _, piece := range s.applyWeeklyCap(&regularElapsed) {
			res.Segments = append(res.Segments, piece)
			switch piece.Category {
			case Regular:
				res.RegularMinutes += int(piece.End.Sub(piece.Start) / time.Minute)
			case Overtime:
				res.OvertimeMinutes += int(piece.End.Sub(piece.Start) / time.Minute)
			case Double:
				res.DoubleMinutes += int(piece.End.Sub(piece.Start) / time.Minute)
			}
		}
	}
	return res
}

// applyWeeklyCap 将单个段按周 40 小时上限切分，正常超出部分改记加班。
func (s Segment) applyWeeklyCap(elapsed *time.Duration) []Segment {
	if s.Category != Regular {
		return []Segment{s}
	}
	dur := s.End.Sub(s.Start)
	if *elapsed+dur <= weekRegularCap {
		*elapsed += dur
		return []Segment{s}
	}
	remain := weekRegularCap - *elapsed
	*elapsed += dur
	if remain <= 0 {
		s.Category = Overtime
		return []Segment{s}
	}
	cut := s.Start.Add(remain)
	reg := s
	reg.End = cut
	ot := s
	ot.Start = cut
	ot.Category = Overtime
	return []Segment{reg, ot}
}

// splitByWeek 在周边界（周日 00:00）处把段切开。
func splitByWeek(s Segment) []Segment {
	var out []Segment
	for cursor := s.Start; cursor.Before(s.End); {
		next := s.End
		if boundary := weekStart(cursor).Add(7 * 24 * time.Hour); boundary.Before(next) {
			next = boundary
		}
		piece := s
		piece.Start = cursor
		piece.End = next
		out = append(out, piece)
		cursor = next
	}
	return out
}

// weekStart 返回 t 所在周的周日 00:00（本地时间，不考虑夏令时）。
func weekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	return midnight.AddDate(0, 0, -int(midnight.Weekday()))
}

// recordIDs 返回区间边界的来源打卡号（去重保序）。
func recordIDs(iv interval) []string {
	ids := make([]string, 0, 2)
	if iv.startID != "" {
		ids = append(ids, iv.startID)
	}
	if iv.endID != "" && iv.endID != iv.startID {
		ids = append(ids, iv.endID)
	}
	return ids
}
