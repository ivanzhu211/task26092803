package worktime

import (
	"reflect"
	"testing"
	"time"
)

var loc = time.FixedZone("LOCAL", 8*3600)

func tm(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
	if err != nil {
		panic(err)
	}
	return t
}

func p(id, emp string, typ PunchType, at string) Punch {
	return Punch{ID: id, EmployeeID: emp, Type: typ, Time: tm(at)}
}

func schedP(id, emp, at, sched string) Punch {
	s := tm(sched)
	return Punch{ID: id, EmployeeID: emp, Type: PunchIn, Time: tm(at), ScheduledStart: &s}
}

type segWant struct {
	start string
	end   string
}

type weekWant struct {
	weekStart string
	regular   []segWant
	overtime  []segWant
	double    []segWant
	anomalies []Anomaly
}

var defaultCfg = Config{RoundMinutes: 5, GraceMinutes: 7}

func findWeek(t *testing.T, res []EmployeeResult, emp, weekStart string) WeekResult {
	t.Helper()
	for _, er := range res {
		if er.EmployeeID != emp {
			continue
		}
		for _, w := range er.Weeks {
			if w.WeekStart.Equal(tm(weekStart)) {
				return w
			}
		}
	}
	t.Fatalf("week %s of employee %s not found", weekStart, emp)
	return WeekResult{}
}

func checkSegs(t *testing.T, label string, got []Segment, want []segWant) {
	t.Helper()
	if want == nil {
		want = []segWant{}
	}
	gotS := make([]segWant, 0, len(got))
	for _, s := range got {
		gotS = append(gotS, segWant{
			start: s.Start.Format("2006-01-02 15:04:05"),
			end:   s.End.Format("2006-01-02 15:04:05"),
		})
		if s.Minutes != int(s.End.Sub(s.Start)/time.Minute) {
			t.Errorf("%s: segment %v Minutes=%d inconsistent with start/end", label, s, s.Minutes)
		}
		if len(s.SourceIDs) == 0 {
			t.Errorf("%s: segment %v missing source IDs", label, s)
		}
	}
	if !reflect.DeepEqual(gotS, want) {
		t.Errorf("%s:\n got=%v\nwant=%v", label, gotS, want)
	}
}

func sumMins(segs []Segment) int {
	total := 0
	for _, s := range segs {
		total += s.Minutes
	}
	return total
}

func checkWeek(t *testing.T, res []EmployeeResult, emp string, want weekWant) {
	t.Helper()
	w := findWeek(t, res, emp, want.weekStart)
	checkSegs(t, "regular", w.Regular, want.regular)
	checkSegs(t, "overtime", w.Overtime, want.overtime)
	checkSegs(t, "double", w.Double, want.double)
	if len(w.Anomalies) == 0 && len(want.anomalies) == 0 {
		return
	}
	if !reflect.DeepEqual(w.Anomalies, want.anomalies) {
		t.Errorf("anomalies:\n got=%v\nwant=%v", w.Anomalies, want.anomalies)
	}
}

// 场景1：周六深夜上班、周日早上下班，中途越过 8 小时的夜班。
func TestNightShiftCrossingMidnightAndWeek(t *testing.T) {
	punches := []Punch{
		p("n1", "E1", PunchIn, "2026-09-26 23:00:00"),
		p("n2", "E1", PunchOut, "2026-09-27 08:00:00"),
	}
	res := Compute(defaultCfg, punches, nil)

	// 跨度 9h，无用餐打卡，自动扣除 04:00-04:30。
	// 当日累计：23:00-04:00 正常 5h，04:30-07:30 正常 3h，07:30-08:00 加班。
	checkWeek(t, res, "E1", weekWant{
		weekStart: "2026-09-20 00:00:00",
		regular:   []segWant{{"2026-09-26 23:00:00", "2026-09-27 00:00:00"}},
	})
	checkWeek(t, res, "E1", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular: []segWant{
			{"2026-09-27 00:00:00", "2026-09-27 04:00:00"},
			{"2026-09-27 04:30:00", "2026-09-27 07:30:00"},
		},
		overtime: []segWant{{"2026-09-27 07:30:00", "2026-09-27 08:00:00"}},
	})
}

// 场景2：同一日期两个班次合计超过 12 小时。
func TestTwoShiftsExceeding12Hours(t *testing.T) {
	punches := []Punch{
		p("a1", "E1", PunchIn, "2026-09-28 06:00:00"),
		p("a2", "E1", MealStart, "2026-09-28 11:00:00"),
		p("a3", "E1", MealEnd, "2026-09-28 11:30:00"),
		p("a4", "E1", PunchOut, "2026-09-28 12:30:00"),
		p("b1", "E1", PunchIn, "2026-09-28 14:00:00"),
		p("b2", "E1", MealStart, "2026-09-28 17:00:00"),
		p("b3", "E1", MealEnd, "2026-09-28 17:30:00"),
		p("b4", "E1", PunchOut, "2026-09-28 21:00:00"),
	}
	res := Compute(defaultCfg, punches, nil)

	// 班次1工作 06:00-11:00、11:30-12:30（6h）；班次2工作 14:00-17:00、17:30-21:00（6.5h）。
	// 合计 12.5h：8h 正常，4h 加班，0.5h 双倍。
	checkWeek(t, res, "E1", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular: []segWant{
			{"2026-09-28 06:00:00", "2026-09-28 11:00:00"},
			{"2026-09-28 11:30:00", "2026-09-28 12:30:00"},
			{"2026-09-28 14:00:00", "2026-09-28 16:00:00"},
		},
		overtime: []segWant{
			{"2026-09-28 16:00:00", "2026-09-28 17:00:00"},
			{"2026-09-28 17:30:00", "2026-09-28 20:30:00"},
		},
		double: []segWant{{"2026-09-28 20:30:00", "2026-09-28 21:00:00"}},
	})
}

// 场景3：关闭 8 小时规则后，双倍规则照常执行。
func TestDoubleTimeWithDailyOT8Disabled(t *testing.T) {
	cfg := Config{RoundMinutes: 5, GraceMinutes: 7, DisableDailyOT8: true}
	punches := []Punch{
		p("a1", "E1", PunchIn, "2026-09-28 08:00:00"),
		p("a2", "E1", MealStart, "2026-09-28 12:00:00"),
		p("a3", "E1", MealEnd, "2026-09-28 12:30:00"),
		p("a4", "E1", PunchOut, "2026-09-28 21:00:00"),
	}
	res := Compute(cfg, punches, nil)

	// 工作 08:00-12:00、12:30-21:00 共 12.5h：12h 正常，0.5h 双倍，无加班。
	checkWeek(t, res, "E1", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular: []segWant{
			{"2026-09-28 08:00:00", "2026-09-28 12:00:00"},
			{"2026-09-28 12:30:00", "2026-09-28 20:30:00"},
		},
		double: []segWant{{"2026-09-28 20:30:00", "2026-09-28 21:00:00"}},
	})
}

// 场景4：第 40 小时落在班次中间。
func TestWeekly40HoursMidShift(t *testing.T) {
	var punches []Punch
	// 周一至周四各 8h（08:00-16:30，用餐 12:00-12:30）。
	days := []string{"2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01"}
	for i, d := range days {
		punches = append(punches,
			p("w"+string(rune('a'+i))+"1", "E1", PunchIn, d+" 08:00:00"),
			p("w"+string(rune('a'+i))+"2", "E1", MealStart, d+" 12:00:00"),
			p("w"+string(rune('a'+i))+"3", "E1", MealEnd, d+" 12:30:00"),
			p("w"+string(rune('a'+i))+"4", "E1", PunchOut, d+" 16:30:00"),
		)
	}
	// 周五 4h（08:00-12:00），累计 36h。
	punches = append(punches,
		p("we1", "E1", PunchIn, "2026-10-02 08:00:00"),
		p("we2", "E1", PunchOut, "2026-10-02 12:00:00"),
	)
	// 周六 6h（08:00-14:00），第 40 小时落在 12:00，之后改记加班。
	punches = append(punches,
		p("wf1", "E1", PunchIn, "2026-10-03 08:00:00"),
		p("wf2", "E1", PunchOut, "2026-10-03 14:00:00"),
	)
	res := Compute(defaultCfg, punches, nil)

	wantReg := []segWant{}
	for _, d := range days {
		wantReg = append(wantReg,
			segWant{d + " 08:00:00", d + " 12:00:00"},
			segWant{d + " 12:30:00", d + " 16:30:00"},
		)
	}
	wantReg = append(wantReg,
		segWant{"2026-10-02 08:00:00", "2026-10-02 12:00:00"},
		segWant{"2026-10-03 08:00:00", "2026-10-03 12:00:00"},
	)
	checkWeek(t, res, "E1", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular:   wantReg,
		overtime:  []segWant{{"2026-10-03 12:00:00", "2026-10-03 14:00:00"}},
	})
}

// 场景5：取整恰在正中间向上取；宽限在第 7 分钟前后的边界。
func TestRoundingTieAndGraceBoundary(t *testing.T) {
	punches := []Punch{
		// E1：下班 12:02:30 恰在 5 分钟正中间，向上取整为 12:05。
		p("r1", "E1", PunchIn, "2026-09-28 08:00:00"),
		p("r2", "E1", PunchOut, "2026-09-28 12:02:30"),
		// E2：上班晚 7 分钟整，按排班 08:00 计。
		schedP("g1", "E2", "2026-09-28 08:07:00", "2026-09-28 08:00:00"),
		p("g2", "E2", PunchOut, "2026-09-28 12:00:00"),
		// E3：上班晚 7 分 01 秒，超出宽限，取整为 08:05。
		schedP("h1", "E3", "2026-09-28 08:07:01", "2026-09-28 08:00:00"),
		p("h2", "E3", PunchOut, "2026-09-28 12:00:00"),
	}
	res := Compute(defaultCfg, punches, nil)

	checkWeek(t, res, "E1", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular:   []segWant{{"2026-09-28 08:00:00", "2026-09-28 12:05:00"}},
	})
	checkWeek(t, res, "E2", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular:   []segWant{{"2026-09-28 08:00:00", "2026-09-28 12:00:00"}},
	})
	checkWeek(t, res, "E3", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular:   []segWant{{"2026-09-28 08:05:00", "2026-09-28 12:00:00"}},
	})
}

// 场景6：重复打卡、缺卡、超过 16 小时、更正与作废。
func TestDedupMissingOverlongCorrectionVoid(t *testing.T) {
	t1730 := tm("2026-09-28 17:30:00")
	t1800 := tm("2026-09-28 18:00:00")
	punches := []Punch{
		p("p1", "E1", PunchIn, "2026-09-28 08:00:00"),
		p("p2", "E1", PunchIn, "2026-09-28 08:01:30"), // 距 p1 不到 2 分钟，丢弃
		p("p3", "E1", PunchOut, "2026-09-28 17:00:00"),
		p("p4", "E1", PunchIn, "2026-09-30 08:00:00"), // 缺下班
		p("p5", "E1", PunchIn, "2026-10-01 08:00:00"),
		p("p6", "E1", PunchOut, "2026-10-01 17:00:00"),
		p("p7", "E1", PunchOut, "2026-10-02 17:00:00"), // 缺上班
		p("p8", "E1", PunchIn, "2026-10-02 20:00:00"),
		p("p9", "E1", PunchOut, "2026-10-03 12:30:00"), // 跨度 16.5h
		p("p10", "E1", PunchIn, "2026-10-03 14:00:00"), // 下班卡被作废，缺下班
		p("p11", "E1", PunchOut, "2026-10-03 18:00:00"),
	}
	corrections := []Correction{
		{TargetID: "p3", SubmittedAt: tm("2026-09-29 10:00:00"), NewTime: &t1800},
		{TargetID: "p3", SubmittedAt: tm("2026-09-29 11:00:00"), NewTime: &t1730}, // 提交最晚，生效
		{TargetID: "p11", SubmittedAt: tm("2026-10-03 12:00:00"), Void: true},
	}
	res := Compute(defaultCfg, punches, corrections)

	// 周一班次 08:00-17:30，自动扣餐 13:00-13:30，工作 9h：8h 正常 + 1h 加班。
	// 周四班次 08:00-17:00，自动扣餐 13:00-13:30，工作 8.5h：8h 正常 + 0.5h 加班。
	checkWeek(t, res, "E1", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular: []segWant{
			{"2026-09-28 08:00:00", "2026-09-28 13:00:00"},
			{"2026-09-28 13:30:00", "2026-09-28 16:30:00"},
			{"2026-10-01 08:00:00", "2026-10-01 13:00:00"},
			{"2026-10-01 13:30:00", "2026-10-01 16:30:00"},
		},
		overtime: []segWant{
			{"2026-09-28 16:30:00", "2026-09-28 17:30:00"},
			{"2026-10-01 16:30:00", "2026-10-01 17:00:00"},
		},
		anomalies: []Anomaly{
			{Reason: "上班打卡缺对应下班打卡", RecordIDs: []string{"p4"}},
			{Reason: "下班打卡缺对应上班打卡", RecordIDs: []string{"p7"}},
			{Reason: "取整后上下班跨度超过16小时", RecordIDs: []string{"p8", "p9"}},
			{Reason: "上班打卡缺对应下班打卡", RecordIDs: []string{"p10"}},
		},
	})
}

// 场景7：自动扣餐的位置——从班次开始满 5 小时起扣 30 分钟；跨度恰好 6 小时不扣。
func TestAutoMealDeductionPosition(t *testing.T) {
	punches := []Punch{
		p("m1", "E1", PunchIn, "2026-09-28 08:00:00"),
		p("m2", "E1", PunchOut, "2026-09-28 17:00:00"),
		p("m3", "E2", PunchIn, "2026-09-28 08:00:00"),
		p("m4", "E2", PunchOut, "2026-09-28 14:00:00"),
	}
	res := Compute(defaultCfg, punches, nil)

	// E1 跨度 9h，扣除 13:00-13:30，工作 8.5h：8h 正常 + 0.5h 加班。
	w1 := findWeek(t, res, "E1", "2026-09-27 00:00:00")
	checkSegs(t, "regular", w1.Regular, []segWant{
		{"2026-09-28 08:00:00", "2026-09-28 13:00:00"},
		{"2026-09-28 13:30:00", "2026-09-28 16:30:00"},
	})
	checkSegs(t, "overtime", w1.Overtime, []segWant{
		{"2026-09-28 16:30:00", "2026-09-28 17:00:00"},
	})
	// 任何片段都不得覆盖被扣除的 13:00-13:30。
	mealStart, mealEnd := tm("2026-09-28 13:00:00"), tm("2026-09-28 13:30:00")
	for _, segs := range [][]Segment{w1.Regular, w1.Overtime, w1.Double} {
		for _, s := range segs {
			if s.Start.Before(mealEnd) && s.End.After(mealStart) {
				t.Errorf("segment %v-%v overlaps deducted meal", s.Start, s.End)
			}
		}
	}

	// E2 跨度恰好 6h，不自动扣餐。
	checkWeek(t, res, "E2", weekWant{
		weekStart: "2026-09-27 00:00:00",
		regular:   []segWant{{"2026-09-28 08:00:00", "2026-09-28 14:00:00"}},
	})
}

// 同一批输入打乱顺序后重算，结果完全一致。
func TestShuffleDeterminism(t *testing.T) {
	t1730 := tm("2026-09-28 17:30:00")
	punches := []Punch{
		p("p1", "E1", PunchIn, "2026-09-28 08:00:00"),
		p("p2", "E1", PunchIn, "2026-09-28 08:01:30"),
		p("p3", "E1", PunchOut, "2026-09-28 17:00:00"),
		p("p4", "E1", PunchIn, "2026-09-30 08:00:00"),
		p("p5", "E1", PunchIn, "2026-10-01 08:00:00"),
		p("p6", "E1", PunchOut, "2026-10-01 17:00:00"),
		p("p7", "E1", PunchOut, "2026-10-02 17:00:00"),
		p("p8", "E1", PunchIn, "2026-10-02 20:00:00"),
		p("p9", "E1", PunchOut, "2026-10-03 12:30:00"),
		p("p10", "E1", PunchIn, "2026-10-03 14:00:00"),
		p("p11", "E1", PunchOut, "2026-10-03 18:00:00"),
		p("q1", "E2", PunchIn, "2026-09-26 23:00:00"),
		p("q2", "E2", PunchOut, "2026-09-27 08:00:00"),
		p("q3", "E2", PunchIn, "2026-09-28 06:00:00"),
		p("q4", "E2", MealStart, "2026-09-28 11:00:00"),
		p("q5", "E2", MealEnd, "2026-09-28 11:30:00"),
		p("q6", "E2", PunchOut, "2026-09-28 14:00:00"),
	}
	corrections := []Correction{
		{TargetID: "p3", SubmittedAt: tm("2026-09-29 11:00:00"), NewTime: &t1730},
		{TargetID: "p11", SubmittedAt: tm("2026-10-03 12:00:00"), Void: true},
	}
	base := Compute(defaultCfg, punches, corrections)

	// 多种确定性重排：逆序、交错、按 ID 排序。
	perms := [][]int{}
	n := len(punches)
	rev := make([]int, n)
	inter := make([]int, 0, n)
	for i := 0; i < n; i++ {
		rev[i] = n - 1 - i
	}
	for i := 0; i < (n+1)/2; i++ {
		inter = append(inter, i)
		if j := n - 1 - i; j != i {
			inter = append(inter, j)
		}
	}
	perms = append(perms, rev, inter)
	for _, perm := range perms {
		shuffled := make([]Punch, 0, n)
		for _, i := range perm {
			shuffled = append(shuffled, punches[i])
		}
		got := Compute(defaultCfg, shuffled, corrections)
		if !reflect.DeepEqual(got, base) {
			t.Fatalf("shuffled input produced different result")
		}
	}
	// 重复计算结果一致。
	if got := Compute(defaultCfg, punches, corrections); !reflect.DeepEqual(got, base) {
		t.Fatalf("repeated computation produced different result")
	}
}

// 不变量：任意一周三类分钟数之和等于该周有效工时，各段互不重叠。
func TestWeeklyTotalsInvariant(t *testing.T) {
	var punches []Punch
	days := []string{"2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01"}
	for i, d := range days {
		punches = append(punches,
			p("w"+string(rune('a'+i))+"1", "E1", PunchIn, d+" 08:00:00"),
			p("w"+string(rune('a'+i))+"2", "E1", MealStart, d+" 12:00:00"),
			p("w"+string(rune('a'+i))+"3", "E1", MealEnd, d+" 12:30:00"),
			p("w"+string(rune('a'+i))+"4", "E1", PunchOut, d+" 16:30:00"),
		)
	}
	punches = append(punches,
		p("we1", "E1", PunchIn, "2026-10-02 08:00:00"),
		p("we2", "E1", PunchOut, "2026-10-02 12:00:00"),
		p("wf1", "E1", PunchIn, "2026-10-03 08:00:00"),
		p("wf2", "E1", PunchOut, "2026-10-03 14:00:00"),
	)
	res := Compute(defaultCfg, punches, nil)
	w := findWeek(t, res, "E1", "2026-09-27 00:00:00")

	// 有效工时：4 天 8h + 4h + 6h = 42h = 2520 分钟。
	if got := sumMins(w.Regular) + sumMins(w.Overtime) + sumMins(w.Double); got != 2520 {
		t.Errorf("total classified minutes = %d, want 2520", got)
	}
	var all []Segment
	all = append(all, w.Regular...)
	all = append(all, w.Overtime...)
	all = append(all, w.Double...)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].Start.Before(all[j].End) && all[j].Start.Before(all[i].End) {
				t.Errorf("segments overlap: %v-%v and %v-%v",
					all[i].Start, all[i].End, all[j].Start, all[j].End)
			}
		}
	}
}

// 性能：3000 名员工两周打卡，一次算完不超过 5 秒。
func TestPerformance(t *testing.T) {
	const employees = 3000
	punches := make([]Punch, 0, employees*14*4)
	for e := 0; e < employees; e++ {
		emp := "EMP" + string(rune(e))
		for d := 0; d < 14; d++ {
			day := tm("2026-09-27 00:00:00").AddDate(0, 0, d).Format("2006-01-02")
			id := func(k string) string { return emp + "-" + day + "-" + k }
			punches = append(punches,
				p(id("i"), emp, PunchIn, day+" 08:00:00"),
				p(id("ms"), emp, MealStart, day+" 12:00:00"),
				p(id("me"), emp, MealEnd, day+" 12:30:00"),
				p(id("o"), emp, PunchOut, day+" 17:00:00"),
			)
		}
	}
	start := time.Now()
	res := Compute(defaultCfg, punches, nil)
	elapsed := time.Since(start)
	if len(res) != employees {
		t.Fatalf("got %d employees, want %d", len(res), employees)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %v, want <= 5s", elapsed)
	}
	t.Logf("computed %d employees, %d punches in %v", employees, len(punches), elapsed)
}
