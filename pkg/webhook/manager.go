package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type WebhookConfig struct {
	Url       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Condition string            `json:"condition"`
	Status    string            `json:"status"`
}

type Manager struct {
	// TODO: allow passing by config
	maxAttempts int
	interval    time.Duration

	config *[]WebhookConfig
	c      *http.Client
	queue  *workerQueue
}

const annotation = "experimental.cloud.rocket.chat/webhook-config"

// everyone owns their webhook config
func ParseAnnotations(annotations map[string]string) (*Manager, error) {
	config := []WebhookConfig{}
	c, exists := annotations[annotation]
	if !exists {
		return &Manager{
			maxAttempts: 5,
			interval:    5 * time.Second,
			config:      &config,
			c:           &http.Client{},
			queue:       newWorkerQueue(),
		}, nil
	}
	if err := json.NewDecoder(strings.NewReader(c)).Decode(&config); err != nil {
		return nil, err
	}

	return &Manager{
		maxAttempts: 5,
		interval:    5 * time.Second,
		config:      &config,
		c:           &http.Client{},
		queue:       newWorkerQueue(),
	}, nil
}

func (m *Manager) Send(ctx context.Context, resource string, condition, status string) error {
	m.queue.enqueue(ctx, resource, func(ctx context.Context) {
		logger := log.FromContext(ctx)

		logger.Info("attempting sending webhook", "condition", condition, "status", status)

		config := m.FindMatchingConfig(condition, status)
		if config == nil {
			logger.Info("no matching config found, skipping webhook", "condition", condition, "status", status)
			return
		}

		logger.Info("found matching config, sending webhook", "url", config.Url)

		errCh := make(chan error, 1)

		go func() {
			defer close(errCh)
			for attempt := range 5 {
				logger.Info("webhook attempt", "attempt", attempt, "url", config.Url)

				req, err := http.NewRequestWithContext(ctx, "POST", config.Url, nil)
				if err != nil {
					errCh <- err
					return
				}

				for key, value := range config.Headers {
					req.Header.Set(key, value)
				}

				resp, err := m.c.Do(req)
				if err != nil {
					if errors.Is(err, context.DeadlineExceeded) {
						m.backoff(attempt)
						continue
					}
					errCh <- err
					return
				}

				content, err := io.ReadAll(resp.Body)
				if err != nil {
					logger.Error(err, "failed to read webhook response body")
				} else {
					logger.Info("webhook response", "body", string(content))
				}
				resp.Body.Close()

				if !isStatusOk(resp.StatusCode) {
					errCh <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
					return
				}
			}
		}()

		select {
		case err := <-errCh:
			logger.Error(err, "failed to send webhook")
		case <-ctx.Done():
			logger.Info("context done, skipping webhook", "condition", condition, "status", status, "Err", ctx.Err())
		}
	})

	return nil
}

func (m *Manager) backoff(attempt int) {
	interval := float64(m.interval)
	exponentialRate := 2.0
	time.Sleep(time.Duration(interval * math.Pow(exponentialRate, float64(attempt))))
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
