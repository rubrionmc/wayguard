package k8s

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	ClientSet *k8s.Clientset
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
	labelSelector := fmt.Sprintf("type=%s", labelType)

	pods, err := c.ClientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})

	if err != nil {
		return nil, err
	}

	return pods.Items, nil
}

func (c *Client) GetPodIPsByLabel(namespace string, labelType string) ([]string, error) {
	labelSelector := fmt.Sprintf("type=%s", labelType)

	pods, err := c.ClientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, pod := range pods.Items {
		if pod.Status.PodIP != "" {
			ips = append(ips, pod.Status.PodIP)
		}
	}

	return ips, nil
}
