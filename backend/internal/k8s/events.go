package k8s

import (
	"context"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
)

// Events 返回与该 Pod 相关的最近事件（最多 30 条，按时间倒序）。
func (s *PodService) Events(ctx context.Context, name string) ([]model.EventView, error) {
	pods, _ := s.podsOf(ctx, name)

	// 事件可能挂在 Deployment 上，也可能挂在真实 Pod 上，这里两者都匹配
	names := map[string]bool{name: true}
	for _, p := range pods {
		names[p.Name] = true
	}

	list, err := s.client.Kube.CoreV1().Events(s.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]model.EventView, 0, 30)
	for _, ev := range list.Items {
		if !names[ev.InvolvedObject.Name] {
			continue
		}
		// 事件时间优先使用 LastTimestamp，其次 EventTime / CreationTimestamp
		ts := ev.LastTimestamp.Unix()
		if ts <= 0 {
			ts = ev.EventTime.Unix()
		}
		if ts <= 0 {
			ts = ev.CreationTimestamp.Unix()
		}
		result = append(result, model.EventView{
			Type:     ev.Type,
			Reason:   ev.Reason,
			Message:  ev.Message,
			Count:    ev.Count,
			LastTime: ts,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].LastTime > result[j].LastTime })
	if len(result) > 30 {
		result = result[:30]
	}
	return result, nil
}
