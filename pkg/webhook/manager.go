package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type WebhookConfig struct {
	Url       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Condition string            `json:"condition"`
	Status    string            `json:"status"`
}

type Manager struct {
	config *[]WebhookConfig
	c      *http.Client
}

const annotation = "experimental.cloud.rocket.chat/webhook-config"

// everyone owns their webhook config
func ParseAnnotations(annotations map[string]string) (*Manager, error) {
	config := []WebhookConfig{}
	c, exists := annotations[annotation]
	if !exists {
		return &Manager{
			config: &config,
			c:      &http.Client{},
		}, nil
	}
	if err := json.NewDecoder(strings.NewReader(c)).Decode(&config); err != nil {
		return nil, err
	}

	return &Manager{
		config: &config,
		c:      &http.Client{},
	}, nil
}

func (m *Manager) Send(ctx context.Context, condition, status string) error {
	logger := log.FromContext(ctx)

	logger.Info("attempting sending webhook", "condition", condition, "status", status)

	config := m.FindMatchingConfig(condition, status)
	if config == nil {
		logger.Info("no matching config found, skipping webhook", "condition", condition, "status", status)
		return nil
	}

	logger.Info("found matching config, sending webhook", "url", config.Url)

	req, err := http.NewRequestWithContext(ctx, "POST", config.Url, nil)
	if err != nil {
		return err
	}

	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := m.c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !isStatusOk(resp.StatusCode) {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func isStatusOk(status int) bool {
	return status >= 200 && status < 300
}

func (m *Manager) FindMatchingConfig(condition, status string) *WebhookConfig {
	if len(*m.config) == 0 {
		return nil
	}
	for _, config := range *m.config {
		if config.Condition == condition && config.Status == status {
			return &config
		}
	}
	return nil
}

func NewConfig(url string, headers map[string]string, condition string, status string) *WebhookConfig {
	return &WebhookConfig{
		Url:       url,
		Headers:   headers,
		Condition: condition,
		Status:    status,
	}
}

func EncodeAnnotation(configs ...*WebhookConfig) (map[string]string, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(configs)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		annotation: string(encoded),
	}, nil
}
