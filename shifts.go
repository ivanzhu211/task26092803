package worktime

import (
	"sort"
	"time"
)

// interval 一段有效工时区间，边界记录来源打卡号。
type interval struct {
	start   time.Time
	end     time.Time
	startID string
	endID   string
}

// shift 一次上班与之后下班组成的班次（取整后时间）。
type shift struct {
	in    normPunch
	out   normPunch
	meals []normPunch // 落在班次内的用餐打卡
}

const maxShiftSpan = 16 * time.Hour

// buildShifts 将上下班打卡配对成班次，产出有效班次与异常。
// 缺下班、缺上班、取整后跨度超过 16 小时的打卡列为异常。
func buildShifts(punches []normPunch) ([]shift, []Anomaly) {
	events := make([]normPunch, 0, len(punches))
	for _, p := range punches {
		if p.typ == PunchIn || p.typ == PunchOut {
			events = append(events, p)
		}
	}
	// 取整后时间相同则下班先于上班，保证背靠背班次正确闭合。
	sort.Slice(events, func(i, j int) bool {
		if !events[i].rounded.Equal(events[j].rounded) {
			return events[i].rounded.Before(events[j].rounded)
		}
		if events[i].typ != events[j].typ {
			return events[i].typ == PunchOut
		}
		return events[i].id < events[j].id
	})

	var shifts []shift
	var anomalies []Anomaly
	var pending *normPunch
	for i := range events {
		e := events[i]
		if e.typ == PunchIn {
			if pending != nil {
				anomalies = append(anomalies, Anomaly{ReasonMissingOut, []string{pending.id}})
			}
			pending = &events[i]
			continue
		}
		if pending == nil {
			anomalies = append(anomalies, Anomaly{ReasonMissingIn, []string{e.id}})
			continue
		}
		s := shift{in: *pending, out: e}
		pending = nil
		if s.out.rounded.Sub(s.in.rounded) > maxShiftSpan {
			anomalies = append(anomalies, Anomaly{ReasonShiftTooLong, []string{s.in.id, s.out.id}})
			continue
		}
		shifts = append(shifts, s)
	}
	if pending != nil {
		anomalies = append(anomalies, Anomaly{ReasonMissingOut, []string{pending.id}})
	}
	return shifts, anomalies
}

// assignMeals 把用餐打卡挂到包含它的班次上；落在任何班次之外的列为异常。
// 应在班次异常判定之后调用，被丢弃班次的用餐打卡同样视为在班次之外。
func assignMeals(punches []normPunch, shifts []shift) []Anomaly {
	var anomalies []Anomaly
	for _, p := range punches {
		if p.typ != MealStart && p.typ != MealEnd {
			continue
		}
		placed := false
		for i := range shifts {
			if !p.rounded.Before(shifts[i].in.rounded) && !p.rounded.After(shifts[i].out.rounded) {
				shifts[i].meals = append(shifts[i].meals, p)
				placed = true
				break
			}
		}
		if !placed {
			anomalies = append(anomalies, Anomaly{ReasonMealOutside, []string{p.id}})
		}
	}
	return anomalies
}

// workIntervals 扣除用餐后班次的有效工时区间。
// 用餐打卡全部成对时按实际取整时长扣除；不成对则全部列为异常，
// 班次按没有用餐打卡处理（跨度超 6 小时自动扣 5 小时起 30 分钟）。
func workIntervals(s shift) ([]interval, []Anomaly) {
	pairs, unpaired := mealPairs(s.meals)
	if len(unpaired) > 0 {
		ids := make([]string, 0, len(unpaired))
		for _, m := range unpaired {
			ids = append(ids, m.id)
		}
		return autoDeduct(s), []Anomaly{{ReasonUnpairedMeal, ids}}
	}
	if len(pairs) == 0 {
		return autoDeduct(s), nil
	}
	ivs := []interval{{s.in.rounded, s.out.rounded, s.in.id, s.out.id}}
	for _, pr := range pairs {
		ivs = subtract(ivs, pr[0].rounded, pr[1].rounded, pr[0].id, pr[1].id)
	}
	return ivs, nil
}

// mealPairs 将班次内用餐打卡按时间配成 (开始,结束) 对。
// 无法严格交替配对时，返回全部用餐打卡为不成对。
func mealPairs(meals []normPunch) (pairs [][2]normPunch, unpaired []normPunch) {
	sorted := make([]normPunch, len(meals))
	copy(sorted, meals)
	// 取整后时间相同则用餐结束先于用餐开始，保证连续用餐正确闭合。
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].rounded.Equal(sorted[j].rounded) {
			return sorted[i].rounded.Before(sorted[j].rounded)
		}
		if sorted[i].typ != sorted[j].typ {
			return sorted[i].typ == MealEnd
		}
		return sorted[i].id < sorted[j].id
	})
	var open *normPunch
	for i := range sorted {
		m := sorted[i]
		if m.typ == MealStart {
			if open != nil {
				return nil, sorted
			}
			open = &sorted[i]
			continue
		}
		if open == nil {
			return nil, sorted
		}
		pairs = append(pairs, [2]normPunch{*open, m})
		open = nil
	}
	if open != nil {
		return nil, sorted
	}
	return pairs, nil
}

// autoDeduct 无有效用餐打卡时：跨度超 6 小时，扣除班次开始满 5 小时起的 30 分钟。
func autoDeduct(s shift) []interval {
	whole := interval{s.in.rounded, s.out.rounded, s.in.id, s.out.id}
	if s.out.rounded.Sub(s.in.rounded) <= 6*time.Hour {
		return []interval{whole}
	}
	return subtract([]interval{whole},
		s.in.rounded.Add(5*time.Hour), s.in.rounded.Add(5*time.Hour+30*time.Minute),
		"", "")
}

// subtract 从区间集中扣除 [ms,me]，切口边界记录来源打卡号。
func subtract(ivs []interval, ms, me time.Time, msID, meID string) []interval {
	out := make([]interval, 0, len(ivs)+1)
	for _, iv := range ivs {
		if !ms.Before(iv.end) || !me.After(iv.start) {
			out = append(out, iv)
			continue
		}
		if ms.After(iv.start) {
			out = append(out, interval{iv.start, ms, iv.startID, msID})
		}
		if me.Before(iv.end) {
			out = append(out, interval{me, iv.end, meID, iv.endID})
		}
	}
	return out
}
