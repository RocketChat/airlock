package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type WebhookConfig struct {
	Group     string            `yaml:"group"`
	Kind      string            `yaml:"kind"`
	Url       string            `yaml:"url"`
	Headers   map[string]string `yaml:"headers"`
	Condition string            `yaml:"condition"`
	Status    string            `yaml:"status"`
}

type WebhookHttpConfig struct {
	Url     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

type Manager struct {
	config map[string]WebhookHttpConfig
	c      *http.Client
}

const annotation = "experimental.cloud.rocket.chat/webhook-config"

// everyone owns their webhook config
func ParseAnnotations(annotations map[string]string) (*Manager, error) {
	config := []WebhookConfig{}
	c, exists := annotations[annotation]
	if !exists {
		return &Manager{
			config: make(map[string]WebhookHttpConfig),
			c:      &http.Client{},
		}, nil
	}
	if err := json.NewDecoder(strings.NewReader(c)).Decode(&config); err != nil {
		return nil, err
	}

	httpConfig := make(map[string]WebhookHttpConfig)
	for _, entry := range config {
		httpConfig[httpConfigKey(entry.Group, entry.Kind)] = WebhookHttpConfig{
			Url:     entry.Url,
			Headers: entry.Headers,
		}
	}
	return &Manager{
		config: httpConfig,
		c:      &http.Client{},
	}, nil
}

func httpConfigKey(group, kind string) string {
	return fmt.Sprintf("%s/%s", group, kind)
}

func (m *Manager) Send(ctx context.Context, group, kind, condition, status string) error {
	if !m.IsConditionMet(group, kind, condition, status) {
		return nil
	}

	config := m.GetWebhookConfig(group, kind)

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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func (m *Manager) IsConditionMet(group, kind, condition, status string) bool {
	if !m.HasWebhookConfig(group, kind) {
		return false
	}
	config := m.GetWebhookConfig(group, kind)
	return config.Condition == condition && config.Status == status
}

func (m *Manager) HasWebhookConfig(group, kind string) bool {
	key := httpConfigKey(group, kind)
	_, ok := m.config[key]
	return ok
}

func (m *Manager) GetWebhookConfig(group, kind string) *WebhookConfig {
	key := httpConfigKey(group, kind)
	config := m.config[key]
	return &WebhookConfig{
		Group:   group,
		Kind:    kind,
		Url:     config.Url,
		Headers: config.Headers,
	}
}

func (m *Manager) GetWebhookConfigAnnotation(group, kind string) map[string]string {
	if !m.HasWebhookConfig(group, kind) {
		return nil
	}
	config := m.GetWebhookConfig(group, kind)
	return map[string]string{
		annotation: fmt.Sprintf(`[{"group": "%s", "kind": "%s", "url": "%s", "headers": %v}]`, group, kind, config.Url, config.Headers),
	}
}
