package k8s

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

const (
	// DomainLabel 标记该 Ingress 由平台托管
	DomainLabel = "ssedeploy.io/domain"
	// 注解：归属关系与展示信息
	domainPodAnnotation    = "ssedeploy.io/pod"
	domainOwnerAnnotation  = "ssedeploy.io/owner-id"
	domainRemarkAnnotation = "ssedeploy.io/remark"
)

var (
	// ErrInvalidDomain 子域名参数不合法（前缀格式、端口、Service 缺失等），映射为 400
	ErrInvalidDomain = errors.New("子域名参数不合法")
	// ErrDomainConflict 域名已被占用，映射为 409
	ErrDomainConflict = errors.New("子域名已被占用")
)

// prefixRe 校验子域名前缀：小写字母/数字开头结尾，中间可含中划线，总长 1-40。
var prefixRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$`)

// reservedPrefixes 系统保留前缀，避免与平台自身或常见入口冲突。
var reservedPrefixes = map[string]bool{
	"www": true, "api": true, "admin": true, "console": true,
	"traefik": true, "ssedeploy": true, "mail": true,
}

// IngressService 负责子域名映射：为 Pod 创建 / 更新 / 删除 Ingress。
type IngressService struct {
	client       *Client
	namespace    string
	baseDomain   string
	ingressClass string
}

// NewIngressService 创建子域名服务；baseDomain 支持 "*.example.com" 写法，会自动去掉 "*."。
func NewIngressService(client *Client, baseDomain, ingressClass string) *IngressService {
	baseDomain = strings.TrimPrefix(strings.TrimSpace(baseDomain), "*.")
	baseDomain = strings.Trim(baseDomain, ".")
	if baseDomain == "" {
		baseDomain = "example.com"
	}
	if ingressClass == "" {
		ingressClass = "traefik"
	}
	return &IngressService{
		client:       client,
		namespace:    client.Namespace,
		baseDomain:   baseDomain,
		ingressClass: ingressClass,
	}
}

// BaseDomain 返回配置的主域名，前端用它实时预览完整域名。
func (s *IngressService) BaseDomain() string { return s.baseDomain }

// ingressNameOf 生成 Ingress 名称：同一 Pod 下不同前缀互不冲突。
func ingressNameOf(podName, prefix string) string { return podName + "-" + prefix }

// Create 创建或更新子域名映射（同一 Pod + 前缀视为更新）。
// 冲突检测覆盖所有命名空间，确保同一个 host 只被一个 Ingress 占用。
func (s *IngressService) Create(ctx context.Context, podName string, ownerID int64, req model.CreateDomainRequest) (*model.DomainMapping, error) {
	prefix := strings.ToLower(strings.TrimSpace(req.Prefix))
	if !prefixRe.MatchString(prefix) {
		return nil, fmt.Errorf("%w: 前缀仅允许小写字母、数字与中划线，长度 1-40，且不能以中划线开头或结尾", ErrInvalidDomain)
	}
	if reservedPrefixes[prefix] {
		return nil, fmt.Errorf("%w: 前缀 %s 为系统保留", ErrInvalidDomain, prefix)
	}
	if req.Port < 0 || req.Port > 65535 {
		return nil, fmt.Errorf("%w: 端口需在 1-65535 之间", ErrInvalidDomain)
	}

	host := prefix + "." + s.baseDomain
	if len(host) > 253 {
		return nil, fmt.Errorf("%w: 域名长度超出限制", ErrInvalidDomain)
	}

	// Ingress 的后端必须是该 Pod 对应的 Service（优先 <pod>-web，兼容手工 Service）
	svc, err := s.serviceForPod(ctx, podName)
	if err != nil {
		return nil, err
	}

	port := req.Port
	if port == 0 {
		port = svc.Spec.Ports[0].Port
	}
	if !serviceHasPort(svc, port) {
		return nil, fmt.Errorf("%w: Service %s 未暴露端口 %d（可用端口：%s）",
			ErrInvalidDomain, svc.Name, port, portsOf(svc))
	}

	name := ingressNameOf(podName, prefix)
	if err := s.ensureHostFree(ctx, host, name); err != nil {
		return nil, err
	}

	remark := strings.TrimSpace(req.Remark)
	if runes := []rune(remark); len(runes) > 100 {
		remark = string(runes[:100])
	}

	class := s.ingressClass
	pathType := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
			Labels: map[string]string{
				appLabel:    podName,
				DomainLabel: "true",
			},
			Annotations: map[string]string{
				domainPodAnnotation:    podName,
				domainOwnerAnnotation:  fmt.Sprintf("%d", ownerID),
				domainRemarkAnnotation: remark,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &class,
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: svc.Name,
											Port: networkingv1.ServiceBackendPort{Number: port},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// 已存在则更新（保留 ResourceVersion，避免 409 冲突）
	existing, err := s.client.Kube.NetworkingV1().Ingresses(s.namespace).Get(ctx, name, metav1.GetOptions{})
	switch {
	case err == nil:
		// 同名 Ingress 不属于该 Pod 时视为域名被占用
		if existing.Annotations[domainPodAnnotation] != podName {
			return nil, fmt.Errorf("%w: %s", ErrDomainConflict, host)
		}
		ing.ResourceVersion = existing.ResourceVersion
		updated, err := s.client.Kube.NetworkingV1().Ingresses(s.namespace).Update(ctx, ing, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("更新 Ingress 失败: %w", err)
		}
		return toDomainMapping(updated, s.baseDomain), nil
	case apierrors.IsNotFound(err):
		created, err := s.client.Kube.NetworkingV1().Ingresses(s.namespace).Create(ctx, ing, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, fmt.Errorf("%w: %s", ErrDomainConflict, host)
			}
			return nil, fmt.Errorf("创建 Ingress 失败: %w", err)
		}
		return toDomainMapping(created, s.baseDomain), nil
	default:
		return nil, fmt.Errorf("读取 Ingress 失败: %w", err)
	}
}

// List 返回某个 Pod 名下的全部子域名映射。
func (s *IngressService) List(ctx context.Context, podName string) ([]*model.DomainMapping, error) {
	list, err := s.client.Kube.NetworkingV1().Ingresses(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: appLabel + "=" + podName,
	})
	if err != nil {
		return nil, fmt.Errorf("读取 Ingress 失败: %w", err)
	}

	result := make([]*model.DomainMapping, 0, len(list.Items))
	for i := range list.Items {
		ing := &list.Items[i]
		// 仅返回归属该 Pod 的映射，避免误展示同名 app 标签的其他资源
		if ing.Annotations[domainPodAnnotation] != podName {
			continue
		}
		result = append(result, toDomainMapping(ing, s.baseDomain))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}

// Delete 删除子域名映射；Ingress 不存在或不属于该 Pod 时返回 ErrNotFound。
func (s *IngressService) Delete(ctx context.Context, podName, prefix string) error {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	name := ingressNameOf(podName, prefix)

	ing, err := s.client.Kube.NetworkingV1().Ingresses(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return store.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取 Ingress 失败: %w", err)
	}
	// 归属校验：只能删除本 Pod 创建的映射
	if ing.Annotations[domainPodAnnotation] != podName {
		return store.ErrNotFound
	}
	return s.client.Kube.NetworkingV1().Ingresses(s.namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ensureHostFree 遍历所有命名空间的 Ingress，保证同一个 host 全局唯一。
// 集群权限不足时降级为当前命名空间内校验，避免直接失败。
func (s *IngressService) ensureHostFree(ctx context.Context, host, selfName string) error {
	list, err := s.client.Kube.NetworkingV1().Ingresses(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if apierrors.IsForbidden(err) {
		list, err = s.client.Kube.NetworkingV1().Ingresses(s.namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return fmt.Errorf("校验域名冲突失败: %w", err)
	}

	for i := range list.Items {
		ing := &list.Items[i]
		if ing.Namespace == s.namespace && ing.Name == selfName {
			continue
		}
		for _, rule := range ing.Spec.Rules {
			if strings.EqualFold(rule.Host, host) {
				return fmt.Errorf("%w: %s 已被 %s/%s 使用", ErrDomainConflict, host, ing.Namespace, ing.Name)
			}
		}
	}
	return nil
}

// serviceForPod 找到 Pod 对应的 Service：优先自动创建的 <pod>-web，其次按 app 标签匹配。
func (s *IngressService) serviceForPod(ctx context.Context, podName string) (*corev1.Service, error) {
	svc, err := s.client.Kube.CoreV1().Services(s.namespace).Get(ctx, serviceName(podName), metav1.GetOptions{})
	if err == nil && len(svc.Spec.Ports) > 0 {
		return svc, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("读取 Service 失败: %w", err)
	}

	list, err := s.client.Kube.CoreV1().Services(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: appLabel + "=" + podName,
	})
	if err != nil {
		return nil, fmt.Errorf("读取 Service 失败: %w", err)
	}
	for i := range list.Items {
		item := &list.Items[i]
		if item.Spec.Selector[appLabel] == podName && len(item.Spec.Ports) > 0 {
			return item, nil
		}
	}
	return nil, fmt.Errorf("%w: Pod %s 还没有可用的 Service，请先启动服务", ErrInvalidDomain, podName)
}

// serviceHasPort 判断 Service 是否暴露了指定端口（按 port 字段匹配）。
func serviceHasPort(svc *corev1.Service, port int32) bool {
	for _, p := range svc.Spec.Ports {
		if p.Port == port {
			return true
		}
	}
	return false
}

// portsOf 拼接 Service 暴露的端口列表，用于错误提示。
func portsOf(svc *corev1.Service) string {
	ports := make([]string, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, fmt.Sprintf("%d", p.Port))
	}
	return strings.Join(ports, ", ")
}

// toDomainMapping 把 Ingress 转换成前端视图。
func toDomainMapping(ing *networkingv1.Ingress, baseDomain string) *model.DomainMapping {
	m := &model.DomainMapping{
		IngressName: ing.Name,
		PodName:     ing.Annotations[domainPodAnnotation],
		Remark:      ing.Annotations[domainRemarkAnnotation],
		CreatedAt:   ing.CreationTimestamp.Unix(),
	}
	if len(ing.Spec.Rules) == 0 {
		return m
	}
	rule := ing.Spec.Rules[0]
	m.Host = rule.Host
	m.Prefix = strings.TrimSuffix(rule.Host, "."+baseDomain)
	if rule.HTTP != nil && len(rule.HTTP.Paths) > 0 {
		if backend := rule.HTTP.Paths[0].Backend.Service; backend != nil {
			m.ServiceName = backend.Name
			m.Port = backend.Port.Number
		}
	}
	return m
}
