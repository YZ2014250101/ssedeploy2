package store

import (
	"ssedeploy/internal/model"
)

// InsertMetric 写入一条资源采样点。
func (s *Store) InsertMetric(namespace, name string, cpuMilli, memBytes, ts int64) error {
	_, err := s.DB.Exec(
		`INSERT INTO metric_samples(namespace, name, cpu_milli, mem_bytes, ts) VALUES(?, ?, ?, ?, ?)`,
		namespace, name, cpuMilli, memBytes, ts,
	)
	return err
}

// QueryMetrics 查询指定时间之后的采样序列，按时间升序供折线图使用。
func (s *Store) QueryMetrics(namespace, name string, since int64) ([]model.MetricPoint, error) {
	rows, err := s.DB.Query(
		`SELECT ts, cpu_milli, mem_bytes FROM metric_samples
		 WHERE namespace = ? AND name = ? AND ts >= ? ORDER BY ts ASC`,
		namespace, name, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]model.MetricPoint, 0, 120)
	for rows.Next() {
		var p model.MetricPoint
		if err := rows.Scan(&p.TS, &p.CPUMilli, &p.MemBytes); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// CleanupMetrics 删除过期采样点，防止表无限增长（默认保留 24 小时）。
func (s *Store) CleanupMetrics(before int64) error {
	_, err := s.DB.Exec(`DELETE FROM metric_samples WHERE ts < ?`, before)
	return err
}
