package k8s

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	ClientSet *k8s.Clientset
}

type PodInfo struct {
	Name  string
	IP    string
	Ready bool
}

type BackendInfo struct {
	Pods      []PodInfo
	Service   string
	Namespace string
	LastCheck time.Time
}

func NewInClusterClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientSet, err := k8s.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Client{ClientSet: clientSet}, nil
}

func IsInClusterEnv() bool {
	_, err := rest.InClusterConfig()
	return err == nil
}

func (c *Client) GetPodsByLabel(namespace string, labelType string) ([]v1.Pod, error) {
	labelSelector := fmt.Sprintf("app=%s", labelType)

	pods, err := c.ClientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})

	if err != nil {
		return nil, err
	}

	return pods.Items, nil
}

func (c *Client) GetPodInfoByLabel(namespace string, labelType string) ([]PodInfo, error) {
	pods, err := c.GetPodsByLabel(namespace, labelType)
	if err != nil {
		return nil, err
	}

	var podInfos []PodInfo
	for _, pod := range pods {
		if pod.Status.PodIP != "" && pod.Status.Phase == v1.PodRunning {
			ready := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
					ready = true
					break
				}
			}

			podInfos = append(podInfos, PodInfo{
				Name:  pod.Name,
				IP:    pod.Status.PodIP,
				Ready: ready,
			})
		}
	}

	return podInfos, nil
}

func (c *Client) GetBackendInfo(namespace string, labelType string, service string) (*BackendInfo, error) {
	podInfos, err := c.GetPodInfoByLabel(namespace, labelType)
	if err != nil {
		return nil, err
	}

	return &BackendInfo{
		Pods:      podInfos,
		Service:   service,
		Namespace: namespace,
		LastCheck: time.Now(),
	}, nil
}

func (c *Client) GetReadyPodIPsByLabel(namespace string, labelType string) ([]string, error) {
	podInfos, err := c.GetPodInfoByLabel(namespace, labelType)
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, podInfo := range podInfos {
		if podInfo.Ready {
			ips = append(ips, podInfo.IP)
		}
	}

	return ips, nil
}
