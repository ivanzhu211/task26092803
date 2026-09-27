package worktime

import (
	"sort"
	"time"
)

// Calculate 对一批打卡记录执行完整计算，结果按员工号排序返回。
// 同一批输入（任意顺序）重复计算结果完全一致。
func Calculate(in Input) []EmployeeResult {
	punches := normalize(in)

	byEmp := map[string][]normPunch{}
	var empOrder []string
	for _, p := range punches {
		if _, ok := byEmp[p.empID]; !ok {
			empOrder = append(empOrder, p.empID)
		}
		byEmp[p.empID] = append(byEmp[p.empID], p)
	}
	sort.Strings(empOrder)

	results := make([]EmployeeResult, 0, len(empOrder))
	for _, emp := range empOrder {
		results = append(results, calcEmployee(emp, byEmp[emp], in.Config))
	}
	return results
}

// calcEmployee 计算单个员工：配对班次、扣餐、日分类、周汇总。
func calcEmployee(emp string, punches []normPunch, cfg Config) EmployeeResult {
	shifts, anomalies := buildShifts(punches)
	anomalies = append(anomalies, assignMeals(punches, shifts)...)

	// 按归班日期（取整后上班时刻所在日期）分组有效区间。
	byDay := map[time.Time][]interval{}
	var dayOrder []time.Time
	for _, s := range shifts {
		ivs, mealAnomalies := workIntervals(s)
		anomalies = append(anomalies, mealAnomalies...)
		day := dayStart(s.in.rounded)
		if _, ok := byDay[day]; !ok {
			dayOrder = append(dayOrder, day)
		}
		byDay[day] = append(byDay[day], ivs...)
	}
	sort.Slice(dayOrder, func(i, j int) bool { return dayOrder[i].Before(dayOrder[j]) })

	var daySegs []Segment
	for _, day := range dayOrder {
		daySegs = append(daySegs, classifyDay(byDay[day], cfg.DailyOTEnabled)...)
	}

	return EmployeeResult{
		EmployeeID: emp,
		Weeks:      applyWeekly(daySegs),
		Anomalies:  anomalies,
	}
}

// dayStart 返回 t 所在日期的 00:00（本地时间）。
func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
