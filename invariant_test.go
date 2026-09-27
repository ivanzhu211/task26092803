package worktime

import (
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// 构造一批含多员工、夜班、更正、作废、异常的复杂输入。
func complexInput() Input {
	punches := []Punch{
		// E1：跨零点跨周夜班。
		p("N1", "E1", PunchIn, tm(10, 3, 22, 0, 0)),
		p("N2", "E1", PunchOut, tm(10, 4, 8, 0, 0)),
		// E2：带成对用餐的白班 + 一次更正。
		p("W1", "E2", PunchIn, tm(9, 28, 8, 3, 0)),
		p("W2", "E2", MealStart, tm(9, 28, 12, 1, 0)),
		p("W3", "E2", MealEnd, tm(9, 28, 12, 44, 0)),
		p("W4", "E2", PunchOut, tm(9, 28, 17, 2, 0)),
		// E3：缺下班 + 重复打卡。
		p("X1", "E3", PunchIn, tm(9, 29, 9, 0, 0)),
		p("X2", "E3", PunchIn, tm(9, 29, 9, 1, 0)),
		// E4：将被作废的下班卡。
		p("V1", "E4", PunchIn, tm(9, 28, 9, 0, 0)),
		p("V2", "E4", PunchOut, tm(9, 28, 17, 0, 0)),
	}
	corrections := []Correction{
		{TargetID: "W1", SubmittedAt: tm(9, 29, 10, 0, 0), NewTime: tm(9, 28, 8, 0, 0)},
		{TargetID: "V2", SubmittedAt: tm(9, 29, 10, 0, 0), Void: true},
	}
	return Input{Config: defaultCfg, Punches: punches, Corrections: corrections}
}

// 同一批输入打乱顺序后重算，结果完全一致。
func TestShuffledInputProducesIdenticalResult(t *testing.T) {
	in := complexInput()
	base := Calculate(in)

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 20; i++ {
		shuffled := in
		shuffled.Punches = append([]Punch(nil), in.Punches...)
		shuffled.Corrections = append([]Correction(nil), in.Corrections...)
		rng.Shuffle(len(shuffled.Punches), func(a, b int) {
			shuffled.Punches[a], shuffled.Punches[b] = shuffled.Punches[b], shuffled.Punches[a]
		})
		rng.Shuffle(len(shuffled.Corrections), func(a, b int) {
			shuffled.Corrections[a], shuffled.Corrections[b] = shuffled.Corrections[b], shuffled.Corrections[a]
		})
		if got := Calculate(shuffled); !reflect.DeepEqual(got, base) {
			t.Fatalf("shuffle %d changed result:\n got: %+v\nwant: %+v", i, got, base)
		}
	}
}

// 不变量：每周三类分钟数之和等于各段时长之和；段互不重叠且按时间排序。
func checkInvariants(t *testing.T, res []EmployeeResult) {
	t.Helper()
	for _, emp := range res {
		for _, w := range emp.Weeks {
			sum := 0
			var prevEnd time.Time
			for i, s := range w.Segments {
				if !s.Start.Before(s.End) {
					t.Errorf("%s: empty segment %+v", emp.EmployeeID, s)
				}
				if i > 0 && s.Start.Before(prevEnd) {
					t.Errorf("%s: overlapping segments at %v", emp.EmployeeID, s.Start)
				}
				prevEnd = s.End
				sum += int(s.End.Sub(s.Start) / time.Minute)
			}
			total := w.RegularMinutes + w.OvertimeMinutes + w.DoubleMinutes
			if total != sum {
				t.Errorf("%s week %s: totals %d != segment sum %d",
					emp.EmployeeID, w.WeekStart, total, sum)
			}
		}
	}
}

func TestInvariants(t *testing.T) {
	checkInvariants(t, Calculate(complexInput()))
}

// 性能：3000 名员工两周打卡（每人每天 8 条），一次算完不超过 5 秒。
func TestPerformance3000Employees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	punches := make([]Punch, 0, 3000*14*4)
	for e := 0; e < 3000; e++ {
		emp := "EMP" + string(rune('A'+e%26)) + string(rune('A'+e/26%26)) + string(rune('0'+e%10))
		for d := 0; d < 14; d++ {
			day := tm(9, 21, 0, 0, 0).AddDate(0, 0, d)
			mk := func(hh, mm int) time.Time {
				return time.Date(2026, day.Month(), day.Day(), hh, mm, 0, 0, loc)
			}
			id := func(n string) string { return emp + "-" + string(rune('a'+d)) + n }
			punches = append(punches,
				p(id("i"), emp, PunchIn, mk(8, 0)),
				p(id("m"), emp, MealStart, mk(12, 0)),
				p(id("n"), emp, MealEnd, mk(12, 30)),
				p(id("o"), emp, PunchOut, mk(17, 0)),
			)
		}
	}
	start := time.Now()
	res := Calculate(Input{Config: defaultCfg, Punches: punches})
	elapsed := time.Since(start)
	if len(res) != 3000 {
		t.Fatalf("got %d employees, want 3000", len(res))
	}
	checkInvariants(t, res)
	if elapsed > 5*time.Second {
		t.Fatalf("took %v, want < 5s", elapsed)
	}
	t.Logf("3000 employees x 14 days in %v", elapsed)
}
