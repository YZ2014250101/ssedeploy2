package model

// PVItem 物理卷（PersistentVolume）展示项。
type PVItem struct {
	Name          string `json:"name"`
	Capacity      string `json:"capacity"`      // 容量，例如 5Gi；缺失时为 "-"
	ReclaimPolicy string `json:"reclaimPolicy"` // Delete / Retain / Recycle
	Status        string `json:"status"`        // Bound / Available / Released / Failed
	ClaimName     string `json:"claimName"`     // 绑定的 PVC，格式 namespace/name；未绑定为空
}

// PVCItem 存储声明（PersistentVolumeClaim）展示项。
type PVCItem struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Capacity  string `json:"capacity"` // 实际分配容量，例如 5Gi
	Status    string `json:"status"`   // Bound / Pending / Lost
}

// StorageClassItem 存储类（StorageClass）展示项。
type StorageClassItem struct {
	Name        string `json:"name"`
	Provisioner string `json:"provisioner"`
}

// StorageOverview 管理员存储大盘聚合数据。
type StorageOverview struct {
	PVs            []PVItem           `json:"pvs"`
	PVCs           []PVCItem          `json:"pvcs"`
	StorageClasses []StorageClassItem `json:"storageClasses"`
}
