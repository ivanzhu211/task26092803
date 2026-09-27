package worktime

import (
	"sort"
	"time"
)

// class 工时类别。
type class int

const (
	cRegular class = iota
	cOvertime
	cDouble
)

// piece 分类后的一段工时。
type piece struct {
	interval
	cls    class
	srcIDs []string
}

const (
	dailyOT8  = 8 * time.Hour
	dailyDT12 = 12 * time.Hour
	weeklyCap = 40 * time.Hour
)

// weekStart 返回 t 所在周的周日 0 点（本地时间）。
func weekStart(t time.Time) time.Time {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return midnight.AddDate(0, 0, -int(midnight.Weekday()))
}

// classify 对单个员工的班次做每日 8/12 分类、跨周切分与每周 40 小时重分类。
func classify(shifts []shift, cfg Config, anomalies []Anomaly) []WeekResult {
	pieces := dailyClassify(shifts, cfg)
	pieces = splitByWeek(pieces)
	weeks := weeklyClassify(pieces)

	keys := make([]time.Time, 0, len(weeks))
	for k := range weeks {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

	results := make([]WeekResult, 0, len(keys))
	for _, k := range keys {
		wr := WeekResult{WeekStart: k}
		for _, p := range weeks[k] {
			seg := Segment{
				Start:     p.start,
				End:       p.end,
				Minutes:   int(p.end.Sub(p.start) / time.Minute),
				SourceIDs: p.srcIDs,
			}
			switch p.cls {
			case cRegular:
				wr.Regular = append(wr.Regular, seg)
			case cOvertime:
				wr.Overtime = append(wr.Overtime, seg)
			case cDouble:
				wr.Double = append(wr.Double, seg)
			}
		}
		results = append(results, wr)
	}

	weekIndex := make(map[time.Time]int, len(results))
	for i, r := range results {
		weekIndex[r.WeekStart] = i
	}
	for _, a := range anomalies {
		ws := weekStart(a.at)
		i, ok := weekIndex[ws]
		if !ok {
			results = append(results, WeekResult{WeekStart: ws})
			i = len(results) - 1
			weekIndex[ws] = i
		}
		a.at = time.Time{}
		results[i].Anomalies = append(results[i].Anomalies, a)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].WeekStart.Before(results[j].WeekStart) })
	return results
}

// dailyClassify 按班次开始日期归并当日工时，超过 8 小时记加班、超过 12 小时记双倍。
func dailyClassify(shifts []shift, cfg Config) []piece {
	sorted := make([]shift, len(shifts))
	copy(sorted, shifts)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].start.Equal(sorted[j].start) {
			return sorted[i].start.Before(sorted[j].start)
		}
		return sorted[i].inID < sorted[j].inID
	})

	type dayKey struct{ y, m, d int }
	dayOf := func(t time.Time) dayKey {
		return dayKey{t.Year(), int(t.Month()), t.Day()}
	}

	var pieces []piece
	var cum time.Duration
	var curDay dayKey
	started := false
	for _, s := range sorted {
		// 班次整体归入取整后上班时刻所在的日期。
		d := dayOf(s.start)
		if !started || d != curDay {
			cum = 0
			curDay = d
			started = true
		}
		for _, w := range s.work {
			pieces = append(pieces, splitDaily(w, &cum, cfg, s.srcIDs)...)
		}
	}
	return pieces
}

// splitDaily 将一段工作区间按当日累计量切成正常/加班/双倍片段。
func splitDaily(w interval, cum *time.Duration, cfg Config, srcIDs []string) []piece {
	var out []piece
	emit := func(a, b time.Time, cls class) {
		if b.After(a) {
			out = append(out, piece{interval{a, b}, cls, srcIDs})
		}
	}
	cur := w.start
	for cur.Before(w.end) {
		cls := cRegular
		var limit time.Duration = -1
		switch {
		case cfg.DisableDailyOT8:
			if *cum >= dailyDT12 {
				cls = cDouble
			} else {
				limit = dailyDT12 - *cum
			}
		default:
			switch {
			case *cum >= dailyDT12:
				cls = cDouble
			case *cum >= dailyOT8:
				cls = cOvertime
				limit = dailyDT12 - *cum
			default:
				limit = dailyOT8 - *cum
			}
		}
		next := w.end
		if limit >= 0 && cur.Add(limit).Before(next) {
			next = cur.Add(limit)
		}
		emit(cur, next, cls)
		*cum += next.Sub(cur)
		cur = next
	}
	return out
}

// splitByWeek 将片段在每周日 0 点处切开。
func splitByWeek(pieces []piece) []piece {
	var out []piece
	for _, p := range pieces {
		cur := p.start
		for cur.Before(p.end) {
			next := weekStart(cur).AddDate(0, 0, 7)
			if next.After(p.end) {
				next = p.end
			}
			out = append(out, piece{interval{cur, next}, p.cls, p.srcIDs})
			cur = next
		}
	}
	return out
}

// weeklyClassify 按周分组，一周内正常工时累计超过 40 小时的部分改记加班。
func weeklyClassify(pieces []piece) map[time.Time][]piece {
	byWeek := make(map[time.Time][]piece)
	for _, p := range pieces {
		ws := weekStart(p.start)
		byWeek[ws] = append(byWeek[ws], p)
	}
	for ws, ps := range byWeek {
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].start.Before(ps[j].start) })
		var regCum time.Duration
		var out []piece
		for _, p := range ps {
			if p.cls != cRegular {
				out = append(out, p)
				continue
			}
			dur := p.end.Sub(p.start)
			if regCum+dur <= weeklyCap {
				regCum += dur
				out = append(out, p)
				continue
			}
			remain := weeklyCap - regCum
			if remain > 0 {
				out = append(out, piece{interval{p.start, p.start.Add(remain)}, cRegular, p.srcIDs})
				regCum = weeklyCap
			}
			out = append(out, piece{interval{p.start.Add(remain), p.end}, cOvertime, p.srcIDs})
		}
		byWeek[ws] = out
	}
	return byWeek
}
