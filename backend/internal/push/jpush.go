package push

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const jPushEndpoint = "https://api.jpush.cn/v3/push"

type JPushSender struct {
	appKey       string
	masterSecret string
	httpClient   *http.Client
}

func NewJPushSenderFromEnv() *JPushSender {
	return &JPushSender{
		appKey:       strings.TrimSpace(os.Getenv("JPUSH_APP_KEY")),
		masterSecret: strings.TrimSpace(os.Getenv("JPUSH_MASTER_SECRET")),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *JPushSender) SendAndroid(ctx context.Context, req SendRequest) error {
	if len(req.RegistrationIDs) == 0 {
		return errors.New("missing registration ids")
	}
	if s == nil || s.appKey == "" || s.masterSecret == "" {
		return errors.New("jpush credentials not configured")
	}
	body, err := json.Marshal(map[string]any{
		"platform": "android",
		"audience": map[string]any{
			"registration_id": req.RegistrationIDs,
		},
		"notification": map[string]any{
			"android": map[string]any{
				"alert":                 req.Body,
				"title":                 req.Title,
				"builder_id":            1,
				"channel_id":            req.ChannelID,
				"extras":                req.Extras,
				"show_badge":            true,
				"badge_add_num":         1,
				"large_icon":            "",
				"priority":              0,
				"style":                 0,
				"alert_type":            7,
				"notification_id":       req.NotificationID,
				"intent":                map[string]any{},
				"uri_activity":          "",
				"uri_action":            "",
				"push_translation":      false,
				"display_foreground":    true,
				"inbox":                 map[string]any{},
				"big_text":              req.Body,
				"big_pic_path":          "",
				"category":              "msg",
				"small_icon":            "",
				"sound":                 "default",
				"set_disable_badge":     false,
				"set_hw_badge_class":    "",
				"set_hw_badge_num":      0,
				"set_vivo_push_mode":    0,
				"set_vivo_class":        "",
				"set_google_title":      req.Title,
				"set_google_body":       req.Body,
				"set_google_small_icon": "",
			},
		},
		"options": map[string]any{
			"time_to_live":        86400,
			"apns_production":     false,
			"big_push_duration":   0,
			"override_msg_id":     0,
			"third_party_channel": map[string]any{},
			"classification":      0,
			"intent":              map[string]any{},
		},
	})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, jPushEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", s.appKey, s.masterSecret))))
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jpush send failed: status=%d", resp.StatusCode)
	}
	return nil
}
