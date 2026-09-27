package worktime

import (
	"sort"
	"time"
)

// normalize 应用全部更正后按规则去重，返回按时间排序的有效打卡。
// 去重规则：同一员工的同类打卡，按取整前时间计，
// 距上一条保留下来的同类打卡不超过 2 分钟的丢弃。
func normalize(punches []Punch, corrections []Correction) []Punch {
	byID := make(map[string]Punch, len(punches))
	for _, p := range punches {
		byID[p.ID] = p
	}

	sortedCorr := make([]Correction, len(corrections))
	copy(sortedCorr, corrections)
	sort.SliceStable(sortedCorr, func(i, j int) bool {
		return sortedCorr[i].SubmittedAt.Before(sortedCorr[j].SubmittedAt)
	})
	for _, c := range sortedCorr {
		p, ok := byID[c.TargetID]
		if !ok {
			continue
		}
		if c.Void {
			delete(byID, c.TargetID)
			continue
		}
		if c.NewTime != nil {
			p.Time = *c.NewTime
			byID[c.TargetID] = p
		}
	}

	surviving := make([]Punch, 0, len(byID))
	for _, p := range byID {
		surviving = append(surviving, p)
	}
	sort.SliceStable(surviving, func(i, j int) bool {
		if !surviving[i].Time.Equal(surviving[j].Time) {
			return surviving[i].Time.Before(surviving[j].Time)
		}
		return surviving[i].ID < surviving[j].ID
	})

	type empType struct {
		emp string
		typ PunchType
	}
	lastKept := make(map[empType]time.Time)
	kept := make([]Punch, 0, len(surviving))
	for _, p := range surviving {
		key := empType{p.EmployeeID, p.Type}
		if last, ok := lastKept[key]; ok && !p.Time.After(last.Add(2*time.Minute)) {
			continue
		}
		lastKept[key] = p.Time
		kept = append(kept, p)
	}
	return kept
}
