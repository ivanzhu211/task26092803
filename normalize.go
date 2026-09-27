package worktime

import (
	"sort"
	"time"
)

// normPunch 归一化后的打卡：已应用更正、去重，并完成取整/宽限。
type normPunch struct {
	id      string
	empID   string
	typ     PunchType
	raw     time.Time // 取整前时间（更正后）
	rounded time.Time // 取整后（或宽限后的排班）时间
}

// normalize 应用更正、按同类 2 分钟去重、取整与排班宽限。
// 输出按 (员工, 取整前时间, 记录号) 排序，保证与输入顺序无关。
func normalize(in Input) []normPunch {
	punches := applyCorrections(in.Punches, in.Corrections)
	punches = dedup(punches)

	out := make([]normPunch, 0, len(punches))
	for _, p := range punches {
		out = append(out, normPunch{
			id:      p.ID,
			empID:   p.EmployeeID,
			typ:     p.Type,
			raw:     p.Time,
			rounded: effectiveTime(p, in.Config.RoundingMinutes),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].empID != out[j].empID {
			return out[i].empID < out[j].empID
		}
		if !out[i].raw.Equal(out[j].raw) {
			return out[i].raw.Before(out[j].raw)
		}
		return out[i].id < out[j].id
	})
	return out
}

// applyCorrections 应用更正：同一记录多次更正取提交时间最晚者；
// 作废则删除记录，否则替换时间。指向未知记录的更正忽略。
func applyCorrections(punches []Punch, corrections []Correction) []Punch {
	latest := map[string]Correction{}
	for _, c := range corrections {
		prev, ok := latest[c.TargetID]
		if !ok || lessCorrection(prev, c) {
			latest[c.TargetID] = c
		}
	}
	out := make([]Punch, 0, len(punches))
	for _, p := range punches {
		c, ok := latest[p.ID]
		if !ok {
			out = append(out, p)
			continue
		}
		if c.Void {
			continue
		}
		p.Time = c.NewTime
		out = append(out, p)
	}
	return out
}

// lessCorrection 决定两条针对同一记录的更正的先后，保证平局时结果确定。
func lessCorrection(a, b Correction) bool {
	if !a.SubmittedAt.Equal(b.SubmittedAt) {
		return a.SubmittedAt.Before(b.SubmittedAt)
	}
	if a.Void != b.Void {
		return b.Void // 提交时间相同，作废优先
	}
	return a.NewTime.Before(b.NewTime)
}

// dedup 同一员工同类打卡，按取整前时间距上一条保留的不超过 2 分钟则丢弃。
func dedup(punches []Punch) []Punch {
	sorted := make([]Punch, len(punches))
	copy(sorted, punches)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].EmployeeID != sorted[j].EmployeeID {
			return sorted[i].EmployeeID < sorted[j].EmployeeID
		}
		if sorted[i].Type != sorted[j].Type {
			return sorted[i].Type < sorted[j].Type
		}
		if !sorted[i].Time.Equal(sorted[j].Time) {
			return sorted[i].Time.Before(sorted[j].Time)
		}
		return sorted[i].ID < sorted[j].ID
	})

	type key struct {
		emp string
		typ PunchType
	}
	lastKept := map[key]time.Time{}
	out := make([]Punch, 0, len(sorted))
	for _, p := range sorted {
		k := key{p.EmployeeID, p.Type}
		if t, ok := lastKept[k]; ok && p.Time.Sub(t) <= 2*time.Minute {
			continue
		}
		lastKept[k] = p.Time
		out = append(out, p)
	}
	return out
}

// effectiveTime 上班打卡晚于排班开始不超过 7 分钟的按排班开始计，不再取整；
// 其余按单位就近取整。
func effectiveTime(p Punch, unitMin int) time.Time {
	if p.Type == PunchIn && p.HasSched {
		d := p.Time.Sub(p.SchedStart)
		if d >= 0 && d <= 7*time.Minute {
			return p.SchedStart
		}
	}
	return roundTime(p.Time, unitMin)
}

// roundTime 按 unitMin 分钟就近取整，恰在正中间时向上取。
func roundTime(t time.Time, unitMin int) time.Time {
	unit := int64(unitMin) * 60
	sec := t.Unix()
	rounded := (sec + unit/2) / unit * unit
	return time.Unix(rounded, 0).In(t.Location())
}
