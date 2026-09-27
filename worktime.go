package worktime

import "sort"

// Compute 计算一批打卡记录的工时分类结果，按员工、按周返回。
// 输入顺序任意，同一批输入重复计算结果完全一致。
func Compute(cfg Config, punches []Punch, corrections []Correction) []EmployeeResult {
	kept := normalize(punches, corrections)

	byEmp := make(map[string][]Punch)
	for _, p := range kept {
		byEmp[p.EmployeeID] = append(byEmp[p.EmployeeID], p)
	}

	emps := make([]string, 0, len(byEmp))
	for id := range byEmp {
		emps = append(emps, id)
	}
	sort.Strings(emps)

	results := make([]EmployeeResult, 0, len(emps))
	for _, id := range emps {
		shifts, anomalies := buildShifts(byEmp[id], cfg)
		results = append(results, EmployeeResult{
			EmployeeID: id,
			Weeks:      classify(shifts, cfg, anomalies),
		})
	}
	return results
}
