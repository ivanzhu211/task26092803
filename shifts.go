package worktime

import (
	"sort"
	"time"
)

// interval 一段起止时刻。
type interval struct {
	start time.Time
	end   time.Time
}

// timedPunch 一条打卡及其取整后的计薪时刻。
type timedPunch struct {
	p   Punch
	eff time.Time
}

// mealPair 一对成对的用餐打卡及其区间。
type mealPair struct {
	interval
	ids [2]string
}

// shift 内部班次结构：起止为取整后时刻，work 为扣除用餐后的工作区间。
type shift struct {
	inID   string
	outID  string
	start  time.Time
	end    time.Time
	work   []interval
	srcIDs []string
}

const maxShiftSpan = 16 * time.Hour

// buildShifts 将单个员工的打卡配对成班次并扣除用餐时间，返回班次与异常。
// punches 须为同一员工的记录，顺序任意。
func buildShifts(punches []Punch, cfg Config) ([]shift, []Anomaly) {
	var events []timedPunch
	var meals []timedPunch
	for _, p := range punches {
		switch p.Type {
		case PunchIn:
			events = append(events, timedPunch{p, effectiveIn(p, cfg)})
		case PunchOut:
			events = append(events, timedPunch{p, roundTime(p.Time, cfg.RoundMinutes)})
		case MealStart, MealEnd:
			meals = append(meals, timedPunch{p, roundTime(p.Time, cfg.RoundMinutes)})
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].eff.Equal(events[j].eff) {
			return events[i].eff.Before(events[j].eff)
		}
		return events[i].p.ID < events[j].p.ID
	})
	sort.SliceStable(meals, func(i, j int) bool {
		if !meals[i].eff.Equal(meals[j].eff) {
			return meals[i].eff.Before(meals[j].eff)
		}
		return meals[i].p.ID < meals[j].p.ID
	})

	var anomalies []Anomaly
	var shifts []shift
	var open *timedPunch
	for i := range events {
		e := events[i]
		if e.p.Type == PunchIn {
			if open != nil {
				anomalies = append(anomalies, Anomaly{
					Reason:    "上班打卡缺对应下班打卡",
					RecordIDs: []string{open.p.ID},
					at:        open.eff,
				})
			}
			open = &events[i]
			continue
		}
		if open == nil {
			anomalies = append(anomalies, Anomaly{
				Reason:    "下班打卡缺对应上班打卡",
				RecordIDs: []string{e.p.ID},
				at:        e.eff,
			})
			continue
		}
		if e.eff.Sub(open.eff) > maxShiftSpan {
			anomalies = append(anomalies, Anomaly{
				Reason:    "取整后上下班跨度超过16小时",
				RecordIDs: []string{open.p.ID, e.p.ID},
				at:        open.eff,
			})
		} else {
			shifts = append(shifts, shift{
				inID:   open.p.ID,
				outID:  e.p.ID,
				start:  open.eff,
				end:    e.eff,
				srcIDs: []string{open.p.ID, e.p.ID},
			})
		}
		open = nil
	}
	if open != nil {
		anomalies = append(anomalies, Anomaly{
			Reason:    "上班打卡缺对应下班打卡",
			RecordIDs: []string{open.p.ID},
			at:        open.eff,
		})
	}

	pairs, unpaired := pairMeals(meals)
	shifts, mealAnomalies := applyMeals(shifts, pairs, unpaired)
	anomalies = append(anomalies, mealAnomalies...)
	return shifts, anomalies
}

// pairMeals 将用餐打卡按时间配对，返回成对区间与不成对的打卡。
func pairMeals(meals []timedPunch) ([]mealPair, []timedPunch) {
	var pairs []mealPair
	var unpaired []timedPunch
	var open *timedPunch
	for i := range meals {
		m := meals[i]
		if m.p.Type == MealStart {
			if open != nil {
				unpaired = append(unpaired, *open)
			}
			open = &meals[i]
			continue
		}
		if open == nil {
			unpaired = append(unpaired, m)
			continue
		}
		pairs = append(pairs, mealPair{
			interval: interval{start: open.eff, end: m.eff},
			ids:      [2]string{open.p.ID, m.p.ID},
		})
		open = nil
	}
	if open != nil {
		unpaired = append(unpaired, *open)
	}
	return pairs, unpaired
}

// applyMeals 将用餐时间分配到班次并扣除，返回扣除后的班次与异常。
// 规则：不成对的用餐打卡列为异常，所在班次按没有用餐打卡处理；
// 落在班次之外的用餐打卡列为异常；
// 没有用餐打卡且跨度超过 6 小时的班次，自动扣除从开始满 5 小时起的 30 分钟。
func applyMeals(shifts []shift, pairs []mealPair, unpaired []timedPunch) ([]shift, []Anomaly) {
	var anomalies []Anomaly

	// 标记含有不成对用餐打卡的班次，这些班次按没有用餐打卡处理。
	tainted := make([]bool, len(shifts))
	for _, u := range unpaired {
		inside := false
		for i, s := range shifts {
			if !u.eff.Before(s.start) && !u.eff.After(s.end) {
				tainted[i] = true
				inside = true
			}
		}
		reason := "用餐打卡不成对"
		if !inside {
			reason = "用餐打卡落在班次之外"
		}
		anomalies = append(anomalies, Anomaly{Reason: reason, RecordIDs: []string{u.p.ID}, at: u.eff})
	}

	// 将成对用餐分配到班次。
	shiftMeals := make([][]interval, len(shifts))
	for _, pr := range pairs {
		assigned := false
		for i, s := range shifts {
			if tainted[i] {
				continue
			}
			if !pr.start.Before(s.start) && !pr.end.After(s.end) {
				shiftMeals[i] = append(shiftMeals[i], pr.interval)
				assigned = true
				break
			}
		}
		if !assigned {
			anomalies = append(anomalies, Anomaly{
				Reason:    "用餐打卡落在班次之外",
				RecordIDs: []string{pr.ids[0], pr.ids[1]},
				at:        pr.start,
			})
		}
	}

	for i, s := range shifts {
		deduct := shiftMeals[i]
		if len(deduct) == 0 && s.end.Sub(s.start) > 6*time.Hour {
			deduct = []interval{{
				start: s.start.Add(5 * time.Hour),
				end:   s.start.Add(5*time.Hour + 30*time.Minute),
			}}
		}
		shifts[i].work = subtract(interval{s.start, s.end}, deduct)
	}
	return shifts, anomalies
}

// subtract 从 base 中扣除若干区间，返回剩余区间（按时间排序）。
func subtract(base interval, cuts []interval) []interval {
	segs := []interval{base}
	for _, c := range cuts {
		var next []interval
		for _, s := range segs {
			if !c.start.Before(s.end) || !c.end.After(s.start) {
				next = append(next, s)
				continue
			}
			if c.start.After(s.start) {
				next = append(next, interval{s.start, c.start})
			}
			if c.end.Before(s.end) {
				next = append(next, interval{c.end, s.end})
			}
		}
		segs = next
	}
	return segs
}
