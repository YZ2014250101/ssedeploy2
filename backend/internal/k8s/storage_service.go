package k8s

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
)

// StorageService 读取集群存储资源（PV / PVC / StorageClass）。
type StorageService struct {
	client *Client
}

// NewStorageService 创建存储服务。
func NewStorageService(client *Client) *StorageService {
	return &StorageService{client: client}
}

// Overview 汇总管理员存储大盘所需的全部数据，结果按名称升序排列。
func (s *StorageService) Overview(ctx context.Context) (*model.StorageOverview, error) {
	result := &model.StorageOverview{
		PVs:            make([]model.PVItem, 0),
		PVCs:           make([]model.PVCItem, 0),
		StorageClasses: make([]model.StorageClassItem, 0),
	}

	// 1. 全部 PersistentVolume（集群级资源）
	pvList, err := s.client.Kube.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("读取 PV 失败: %w", err)
	}
	for i := range pvList.Items {
		pv := &pvList.Items[i]
		// 绑定关系可能为空（Pending / Available），必须判空
		claimName := ""
		if ref := pv.Spec.ClaimRef; ref != nil {
			claimName = ref.Namespace + "/" + ref.Name
		}
		result.PVs = append(result.PVs, model.PVItem{
			Name:          pv.Name,
			Capacity:      quantityString(pv.Spec.Capacity),
			ReclaimPolicy: string(pv.Spec.PersistentVolumeReclaimPolicy),
			Status:        string(pv.Status.Phase),
			ClaimName:     claimName,
		})
	}

	// 2. 所有命名空间的 PersistentVolumeClaim
	pvcList, err := s.client.Kube.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("读取 PVC 失败: %w", err)
	}
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		// 容量取 Status.Capacity（已实际分配），未绑定时可能为空
		result.PVCs = append(result.PVCs, model.PVCItem{
			Name:      pvc.Name,
			Namespace: pvc.Namespace,
			Capacity:  quantityString(pvc.Status.Capacity),
			Status:    string(pvc.Status.Phase),
		})
	}

	// 3. 全部 StorageClass（集群级资源）
	scList, err := s.client.Kube.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("读取 StorageClass 失败: %w", err)
	}
	for i := range scList.Items {
		sc := &scList.Items[i]
		result.StorageClasses = append(result.StorageClasses, model.StorageClassItem{
			Name:        sc.Name,
			Provisioner: sc.Provisioner,
		})
	}

	// 输出顺序稳定，便于前端展示与对比
	sort.Slice(result.PVs, func(i, j int) bool { return result.PVs[i].Name < result.PVs[j].Name })
	sort.Slice(result.PVCs, func(i, j int) bool {
		a, b := result.PVCs[i], result.PVCs[j]
		if a.Namespace == b.Namespace {
			return a.Name < b.Name
		}
		return a.Namespace < b.Namespace
	})
	sort.Slice(result.StorageClasses, func(i, j int) bool {
		return result.StorageClasses[i].Name < result.StorageClasses[j].Name
	})

	return result, nil
}

// quantityString 安全读取 core.ResourceStorage 并转成可读字符串。
// 容量字段缺失时返回 "-"，避免前端出现空单元格。
func quantityString(capacity corev1.ResourceList) string {
	if capacity == nil {
		return "-"
	}
	q, ok := capacity[corev1.ResourceStorage]
	if !ok {
		return "-"
	}
	return q.String()
}
