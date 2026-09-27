package worktime

import (
	"reflect"
	"testing"
	"time"
)

// 场景 5a：取整恰在正中间向上取（5/6/15 分钟单位）。
func TestRoundingHalfUp(t *testing.T) {
	cases := []struct {
		unit int
		in   time.Time
		want time.Time
	}{
		{5, tm(9, 28, 10, 2, 30), tm(9, 28, 10, 5, 0)}, // 正中向上
		{5, tm(9, 28, 10, 2, 29), tm(9, 28, 10, 0, 0)}, // 差 1 秒向下
		{6, tm(9, 28, 10, 3, 0), tm(9, 28, 10, 6, 0)},  // 正中向上
		{6, tm(9, 28, 10, 2, 59), tm(9, 28, 10, 0, 0)},
		{15, tm(9, 28, 10, 7, 30), tm(9, 28, 10, 15, 0)}, // 正中向上
		{15, tm(9, 28, 10, 7, 29), tm(9, 28, 10, 0, 0)},
	}
	for _, c := range cases {
		if got := roundTime(c.in, c.unit); !got.Equal(c.want) {
			t.Errorf("roundTime(%v, %d) = %v, want %v", c.in, c.unit, got, c.want)
		}
	}
}

// 场景 5b：宽限在第 7 分钟前后的边界。
// 排班 09:00：09:07:00 仍按 09:00 计；09:07:01 超出宽限，按 5 分钟取整为 09:05。
func TestGracePeriodBoundary(t *testing.T) {
	sched := tm(9, 28, 9, 0, 0)
	mk := func(id string, t time.Time) Punch {
		return Punch{ID: id, EmployeeID: "E1", Type: PunchIn, Time: t, SchedStart: sched, HasSched: true}
	}
	res := Calculate(Input{
		Config: defaultCfg,
		Punches: []Punch{
			mk("G1", tm(9, 28, 9, 7, 0)), // 恰好 7 分钟，宽限内
			p("O1", "E1", PunchOut, tm(9, 28, 12, 0, 0)),
			mk("G2", tm(9, 29, 9, 7, 1)), // 超出 1 秒，取整为 09:05
			p("O2", "E1", PunchOut, tm(9, 29, 12, 0, 0)),
		},
	})
	emp := findEmp(t, res, "E1")
	w := findWeek(t, emp, tm(9, 27, 0, 0, 0))
	want := []Segment{
		seg(tm(9, 28, 9, 0, 0), tm(9, 28, 12, 0, 0), Regular, "G1", "O1"),
		seg(tm(9, 29, 9, 5, 0), tm(9, 29, 12, 0, 0), Regular, "G2", "O2"),
	}
	if !reflect.DeepEqual(w.Segments, want) {
		t.Errorf("segments = %+v, want %+v", w.Segments, want)
	}
}

// 场景 6a：重复打卡（2 分钟内同类）被丢弃。
func TestDuplicatePunchDiscarded(t *testing.T) {
	res := Calculate(Input{
		Config: defaultCfg,
		Punches: []Punch{
			p("D1", "E1", PunchIn, tm(9, 28, 9, 0, 0)),
			p("D2", "E1", PunchIn, tm(9, 28, 9, 1, 30)), // 距 D1 不到 2 分钟，丢弃
			p("D3", "E1", PunchOut, tm(9, 28, 12, 0, 0)),
		},
	})
	emp := findEmp(t, res, "E1")
	if len(emp.Anomalies) != 0 {
		t.Fatalf("unexpected anomalies: %+v", emp.Anomalies)
	}
	w := findWeek(t, emp, tm(9, 27, 0, 0, 0))
	checkWeek(t, w, 180, 0, 0, []Segment{
		seg(tm(9, 28, 9, 0, 0), tm(9, 28, 12, 0, 0), Regular, "D1", "D3"),
	})
}

// 场景 6b：缺下班、缺上班、跨度超 16 小时。
func TestAnomalousShifts(t *testing.T) {
	res := Calculate(Input{
		Config: defaultCfg,
		Punches: []Punch{
			p("M1", "E2", PunchIn, tm(9, 28, 9, 0, 0)),   // 缺下班
			p("M2", "E3", PunchOut, tm(9, 28, 17, 0, 0)), // 缺上班
			p("L1", "E4", PunchIn, tm(9, 28, 6, 0, 0)),   // 16.5h，超 16h
			p("L2", "E4", PunchOut, tm(9, 28, 22, 30, 0)),
		},
	})
	want := map[string][]Anomaly{
		"E2": {{ReasonMissingOut, []string{"M1"}}},
		"E3": {{ReasonMissingIn, []string{"M2"}}},
		"E4": {{ReasonShiftTooLong, []string{"L1", "L2"}}},
	}
	for empID, anomalies := range want {
		emp := findEmp(t, res, empID)
		if len(emp.Weeks) != 0 {
			t.Errorf("%s: expected no weeks, got %+v", empID, emp.Weeks)
		}
		if !reflect.DeepEqual(emp.Anomalies, anomalies) {
			t.Errorf("%s: anomalies = %+v, want %+v", empID, emp.Anomalies, anomalies)
		}
	}
}

// 场景 6c：更正（多次取最晚提交）与作废。
func TestCorrectionsAndVoid(t *testing.T) {
	res := Calculate(Input{
		Config: defaultCfg,
		Punches: []Punch{
			p("P1", "E5", PunchIn, tm(9, 28, 9, 0, 0)),
			p("P2", "E5", PunchOut, tm(9, 28, 17, 0, 0)),
			p("Q1", "E6", PunchIn, tm(9, 28, 9, 0, 0)),
			p("Q2", "E6", PunchOut, tm(9, 28, 12, 0, 0)),
		},
		Corrections: []Correction{
			// 两次更正 P1，最晚提交（改到 10:00）生效。
			{TargetID: "P1", SubmittedAt: tm(9, 29, 8, 0, 0), NewTime: tm(9, 28, 11, 0, 0)},
			{TargetID: "P1", SubmittedAt: tm(9, 29, 9, 0, 0), NewTime: tm(9, 28, 10, 0, 0)},
			// 作废 Q2，Q1 缺下班。
			{TargetID: "Q2", SubmittedAt: tm(9, 29, 8, 0, 0), Void: true},
		},
	})
	e5 := findEmp(t, res, "E5")
	w := findWeek(t, e5, tm(9, 27, 0, 0, 0))
	// 10:00-17:00（7h），自动扣餐 15:00-15:30。
	checkWeek(t, w, 390, 0, 0, []Segment{
		seg(tm(9, 28, 10, 0, 0), tm(9, 28, 15, 0, 0), Regular, "P1"),
		seg(tm(9, 28, 15, 30, 0), tm(9, 28, 17, 0, 0), Regular, "P2"),
	})

	e6 := findEmp(t, res, "E6")
	if len(e6.Weeks) != 0 {
		t.Errorf("E6: expected no weeks, got %+v", e6.Weeks)
	}
	want := []Anomaly{{ReasonMissingOut, []string{"Q1"}}}
	if !reflect.DeepEqual(e6.Anomalies, want) {
		t.Errorf("E6 anomalies = %+v, want %+v", e6.Anomalies, want)
	}
}

// 场景 7：自动扣餐的位置；成对用餐按实际扣除；不成对与班次外用餐列为异常。
func TestMealDeduction(t *testing.T) {
	res := Calculate(Input{
		Config: defaultCfg,
		Punches: []Punch{
			// E1：无用餐打卡，08:00-18:00，自动扣 13:00-13:30。
			p("A1", "E1", PunchIn, tm(9, 28, 8, 0, 0)),
			p("A2", "E1", PunchOut, tm(9, 28, 18, 0, 0)),
			// E2：成对用餐 12:00-12:45，按实际扣除。
			p("B1", "E2", PunchIn, tm(9, 28, 8, 0, 0)),
			p("B2", "E2", MealStart, tm(9, 28, 12, 0, 0)),
			p("B3", "E2", MealEnd, tm(9, 28, 12, 45, 0)),
			p("B4", "E2", PunchOut, tm(9, 28, 18, 0, 0)),
			// E3：用餐不成对，列异常，按无用餐处理（自动扣 13:00-13:30）。
			p("C1", "E3", PunchIn, tm(9, 28, 8, 0, 0)),
			p("C2", "E3", MealStart, tm(9, 28, 12, 0, 0)),
			p("C3", "E3", PunchOut, tm(9, 28, 18, 0, 0)),
			// E4：用餐打卡落在任何班次之外。
			p("D1", "E4", MealStart, tm(9, 28, 23, 0, 0)),
		},
	})

	e1 := findEmp(t, res, "E1")
	w1 := findWeek(t, e1, tm(9, 27, 0, 0, 0))
	checkWeek(t, w1, 480, 90, 0, []Segment{
		seg(tm(9, 28, 8, 0, 0), tm(9, 28, 13, 0, 0), Regular, "A1"),
		seg(tm(9, 28, 13, 30, 0), tm(9, 28, 16, 30, 0), Regular, "A2"),
		seg(tm(9, 28, 16, 30, 0), tm(9, 28, 18, 0, 0), Overtime, "A2"),
	})

	e2 := findEmp(t, res, "E2")
	w2 := findWeek(t, e2, tm(9, 27, 0, 0, 0))
	checkWeek(t, w2, 480, 75, 0, []Segment{
		seg(tm(9, 28, 8, 0, 0), tm(9, 28, 12, 0, 0), Regular, "B1", "B2"),
		seg(tm(9, 28, 12, 45, 0), tm(9, 28, 16, 45, 0), Regular, "B3", "B4"),
		seg(tm(9, 28, 16, 45, 0), tm(9, 28, 18, 0, 0), Overtime, "B3", "B4"),
	})

	e3 := findEmp(t, res, "E3")
	if !reflect.DeepEqual(e3.Anomalies, []Anomaly{{ReasonUnpairedMeal, []string{"C2"}}}) {
		t.Errorf("E3 anomalies = %+v", e3.Anomalies)
	}
	w3 := findWeek(t, e3, tm(9, 27, 0, 0, 0))
	checkWeek(t, w3, 480, 90, 0, []Segment{
		seg(tm(9, 28, 8, 0, 0), tm(9, 28, 13, 0, 0), Regular, "C1"),
		seg(tm(9, 28, 13, 30, 0), tm(9, 28, 16, 30, 0), Regular, "C3"),
		seg(tm(9, 28, 16, 30, 0), tm(9, 28, 18, 0, 0), Overtime, "C3"),
	})

	e4 := findEmp(t, res, "E4")
	if !reflect.DeepEqual(e4.Anomalies, []Anomaly{{ReasonMealOutside, []string{"D1"}}}) {
		t.Errorf("E4 anomalies = %+v", e4.Anomalies)
	}
}
