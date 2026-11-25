package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/springmusk026/Kiro-Gateway/auth"
	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	microsoftProfileSelectionTTL          = 10 * time.Minute
	microsoftMaxPendingProfileSelections  = 64
	microsoftCanceledSessionTTL           = 10 * time.Minute
	microsoftMaxCanceledSessionTombstones = 128
	microsoftProfileDiscoveryTimeout      = 30 * time.Second
)

// looksLikeKiroAPIKey is a lightweight heuristic for plain-text imports.
// Official keys currently use the ksk_ prefix; future formats can still be
// imported via explicit authMethod/kiroApiKey fields.
func looksLikeKiroAPIKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	// Convenience form: ksk_xxx|region
	if idx := strings.IndexByte(value, '|'); idx > 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return strings.HasPrefix(value, "ksk_")
}

type microsoftProfileSelection struct {
	SessionID string
	Account   config.Account
	Profiles  []KiroProfile
	ExpiresAt time.Time
	timer     *time.Timer
	mu        sync.Mutex
	canceled  atomic.Bool
}

type microsoftProfileDiscovery struct {
	cancel context.CancelFunc
}

func (h *Handler) apiStartIamSso(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartUrl string `json:"startUrl"`
		Region   string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	if req.StartUrl == "" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "startUrl is required"})
		return
	}

	sessionID, authorizeUrl, expiresIn, err := auth.StartIamSsoLogin(req.StartUrl, req.Region)
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId":    sessionID,
		"authorizeUrl": authorizeUrl,
		"expiresIn":    expiresIn,
	})
}

func (h *Handler) apiCompleteIamSso(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID   string `json:"sessionId"`
		CallbackUrl string `json:"callbackUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	accessToken, refreshToken, clientID, clientSecret, region, expiresIn, err := auth.CompleteIamSsoLogin(req.SessionID, req.CallbackUrl)
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// 获取用户信息
	email, _, _ := auth.GetUserInfo(accessToken)

	// 创建账号
	account := config.Account{
		ID:           auth.GenerateAccountID(),
		Email:        email,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthMethod:   "idc",
		Region:       region,
		ExpiresAt:    time.Now().Unix() + int64(expiresIn),
		Enabled:      true,
		MachineId:    config.GenerateMachineId(),
	}

	if err := config.AddAccount(account); err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	h.pool.Reload()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"account": map[string]interface{}{
			"id":    account.ID,
			"email": account.Email,
		},
	})
}

func (h *Handler) apiStartMicrosoftSSO(w http.ResponseWriter, r *http.Request) {
	sessionID, authorizeURL, expiresIn, err := auth.StartMicrosoftSSOLogin()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId":    sessionID,
		"authorizeUrl": authorizeURL,
		"expiresIn":    expiresIn,
		"stage":        "kiro",
	})
}

func (h *Handler) apiCompleteMicrosoftSSO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID   string `json:"sessionId"`
		CallbackURL string `json:"callbackUrl"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.CallbackURL) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "sessionId and callbackUrl are required"})
		return
	}

	progress, err := auth.ContinueMicrosoftSSOLogin(req.SessionID, req.CallbackURL)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if h.microsoftSessionCanceled(req.SessionID) {
		auth.CancelMicrosoftSSOLogin(req.SessionID)
		h.writeMicrosoftSSOCanceled(w)
		return
	}
	if progress.AuthorizationURL != "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"stage":        "microsoft",
			"authorizeUrl": progress.AuthorizationURL,
		})
		return
	}
	if progress.Result == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Microsoft SSO returned no credential"})
		return
	}

	result := progress.Result
	account := config.Account{
		ID:            auth.GenerateAccountID(),
		Email:         result.Email,
		UserId:        result.UserID,
		AccessToken:   result.AccessToken,
		RefreshToken:  result.RefreshToken,
		ClientID:      result.ClientID,
		AuthMethod:    auth.MicrosoftSSOAuthMethod,
		Provider:      auth.MicrosoftSSOProvider,
		Region:        "us-east-1",
		ExpiresAt:     result.ExpiresAt,
		Enabled:       true,
		MachineId:     config.GenerateMachineId(),
		TokenEndpoint: result.TokenEndpoint,
		IssuerURL:     result.IssuerURL,
		Scopes:        result.Scopes,
	}

	discoveryContext, discovery, ok := h.beginMicrosoftProfileDiscovery(r.Context(), req.SessionID)
	if !ok {
		clearMicrosoftAccountCredential(&account)
		h.writeMicrosoftSSOCanceled(w)
		return
	}
	profiles, profileErr := DiscoverKiroProfilesContext(discoveryContext, &account)
	discoveryErr := discoveryContext.Err()
	h.endMicrosoftProfileDiscovery(req.SessionID, discovery)

	h.microsoftFlowMu.Lock()
	if h.microsoftSessionCanceledLocked(req.SessionID, time.Now()) {
		h.microsoftFlowMu.Unlock()
		clearMicrosoftAccountCredential(&account)
		h.writeMicrosoftSSOCanceled(w)
		return
	}
	if discoveryErr != nil {
		h.microsoftFlowMu.Unlock()
		clearMicrosoftAccountCredential(&account)
		w.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(w).Encode(map[string]string{"error": "Microsoft profile discovery was canceled or timed out"})
		return
	}
	if len(profiles) > 1 {
		selectionID, expiredSelections, err := h.storeMicrosoftProfileSelection(req.SessionID, account, profiles)
		h.microsoftFlowMu.Unlock()
		discardDetachedMicrosoftProfileSelections(expiredSelections)
		if err != nil {
			clearMicrosoftAccountCredential(&account)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":                  true,
			"stage":                    "profile",
			"requiresProfileSelection": true,
			"selectionId":              selectionID,
			"profiles":                 profiles,
		})
		return
	}
	if len(profiles) == 1 {
		account.ProfileArn = profiles[0].ARN
	}
	if err := config.AddAccount(account); err != nil {
		h.microsoftFlowMu.Unlock()
		h.writeAddAccountError(w, err)
		return
	}
	delete(h.microsoftCanceled, strings.TrimSpace(req.SessionID))
	h.microsoftFlowMu.Unlock()
	h.pool.Reload()

	response := map[string]interface{}{
		"success": true,
		"stage":   "complete",
		"account": map[string]interface{}{"id": account.ID, "email": account.Email},
	}
	if profileErr != nil {
		response["warning"] = "The account was added, but its Kiro profile could not be resolved yet"
	}
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) apiSelectMicrosoftSSOProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SelectionID string `json:"selectionId"`
		ProfileARN  string `json:"profileArn"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}
	selectionID := strings.TrimSpace(req.SelectionID)
	selection := h.getMicrosoftProfileSelection(selectionID)
	if selection == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Microsoft profile selection not found or expired"})
		return
	}

	selection.mu.Lock()
	if selection.canceled.Load() || !time.Now().Before(selection.ExpiresAt) {
		selection.mu.Unlock()
		h.removeMicrosoftProfileSelection(selectionID, selection)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Microsoft profile selection not found or expired"})
		return
	}

	profileARN := strings.TrimSpace(req.ProfileARN)
	allowed := false
	for _, profile := range selection.Profiles {
		if profile.ARN == profileARN {
			allowed = true
			break
		}
	}
	if !allowed {
		selection.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Selected Kiro profile was not offered for this login"})
		return
	}

	account := selection.Account
	account.ProfileArn = profileARN
	h.microsoftFlowMu.Lock()
	now := time.Now()
	if selection.canceled.Load() ||
		!now.Before(selection.ExpiresAt) ||
		h.microsoftSessionCanceledLocked(selection.SessionID, now) {
		h.microsoftFlowMu.Unlock()
		h.detachMicrosoftProfileSelection(selectionID, selection)
		selection.canceled.Store(true)
		selection.Account = config.Account{}
		selection.Profiles = nil
		selection.mu.Unlock()
		h.writeMicrosoftSSOCanceled(w)
		return
	}
	if err := config.AddAccount(account); err != nil {
		h.microsoftFlowMu.Unlock()
		selection.mu.Unlock()
		h.writeAddAccountError(w, err)
		return
	}
	h.detachMicrosoftProfileSelection(selectionID, selection)
	selection.canceled.Store(true)
	selection.Account = config.Account{}
	selection.Profiles = nil
	delete(h.microsoftCanceled, selection.SessionID)
	h.microsoftFlowMu.Unlock()
	selection.mu.Unlock()
	h.pool.Reload()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"stage":   "complete",
		"account": map[string]interface{}{"id": account.ID, "email": account.Email},
	})
}

func (h *Handler) apiCancelMicrosoftSSO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID   string `json:"sessionId"`
		SelectionID string `json:"selectionId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	selectionID := strings.TrimSpace(req.SelectionID)
	if sessionID == "" && selectionID != "" {
		if selection := h.getMicrosoftProfileSelection(selectionID); selection != nil {
			sessionID = selection.SessionID
		}
	}
	if sessionID != "" {
		h.markMicrosoftSessionCanceled(sessionID)
	}
	auth.CancelMicrosoftSSOLogin(sessionID)
	if selectionID != "" {
		h.removeMicrosoftProfileSelection(selectionID, nil)
	}
	if sessionID != "" {
		h.removeMicrosoftProfileSelectionsForSession(sessionID)
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *Handler) storeMicrosoftProfileSelection(
	sessionID string,
	account config.Account,
	profiles []KiroProfile,
) (string, []*microsoftProfileSelection, error) {
	now := time.Now()
	expiresAt := now.Add(microsoftProfileSelectionTTL)
	tokenExpiry := time.Unix(account.ExpiresAt, 0)
	if account.ExpiresAt > 0 && tokenExpiry.Before(expiresAt) {
		expiresAt = tokenExpiry
	}
	if !expiresAt.After(now) {
		return "", nil, fmt.Errorf("Microsoft credential expired before profile selection")
	}
	selectionID := uuid.NewString()
	selection := &microsoftProfileSelection{
		SessionID: strings.TrimSpace(sessionID),
		Account:   account,
		Profiles:  append([]KiroProfile(nil), profiles...),
		ExpiresAt: expiresAt,
	}
	var expired []*microsoftProfileSelection

	h.microsoftSelectionsMu.Lock()
	if h.microsoftSelections == nil {
		h.microsoftSelections = make(map[string]*microsoftProfileSelection)
	}
	for id, current := range h.microsoftSelections {
		if !now.Before(current.ExpiresAt) {
			delete(h.microsoftSelections, id)
			current.canceled.Store(true)
			if current.timer != nil {
				current.timer.Stop()
				current.timer = nil
			}
			expired = append(expired, current)
		}
	}
	if len(h.microsoftSelections) >= microsoftMaxPendingProfileSelections {
		h.microsoftSelectionsMu.Unlock()
		return "", expired, fmt.Errorf("too many pending Microsoft profile selections; cancel one and try again")
	}
	h.microsoftSelections[selectionID] = selection
	selection.timer = time.AfterFunc(time.Until(expiresAt), func() {
		h.removeMicrosoftProfileSelection(selectionID, selection)
	})
	h.microsoftSelectionsMu.Unlock()
	return selectionID, expired, nil
}

func (h *Handler) getMicrosoftProfileSelection(selectionID string) *microsoftProfileSelection {
	if selectionID == "" {
		return nil
	}
	h.microsoftSelectionsMu.Lock()
	selection := h.microsoftSelections[selectionID]
	if selection != nil && !time.Now().Before(selection.ExpiresAt) {
		delete(h.microsoftSelections, selectionID)
		selection.canceled.Store(true)
		if selection.timer != nil {
			selection.timer.Stop()
			selection.timer = nil
		}
		h.microsoftSelectionsMu.Unlock()
		discardDetachedMicrosoftProfileSelection(selection)
		return nil
	}
	h.microsoftSelectionsMu.Unlock()
	return selection
}

func (h *Handler) detachMicrosoftProfileSelection(
	selectionID string,
	expected *microsoftProfileSelection,
) *microsoftProfileSelection {
	selectionID = strings.TrimSpace(selectionID)
	if selectionID == "" {
		return nil
	}
	h.microsoftSelectionsMu.Lock()
	current := h.microsoftSelections[selectionID]
	if current != nil && (expected == nil || current == expected) {
		delete(h.microsoftSelections, selectionID)
		current.canceled.Store(true)
		if current.timer != nil {
			current.timer.Stop()
			current.timer = nil
		}
	} else {
		current = nil
	}
	h.microsoftSelectionsMu.Unlock()
	return current
}

func (h *Handler) removeMicrosoftProfileSelection(selectionID string, expected *microsoftProfileSelection) {
	if selection := h.detachMicrosoftProfileSelection(selectionID, expected); selection != nil {
		discardDetachedMicrosoftProfileSelection(selection)
	}
}

func (h *Handler) removeMicrosoftProfileSelectionsForSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	var removed []*microsoftProfileSelection
	h.microsoftSelectionsMu.Lock()
	for selectionID, selection := range h.microsoftSelections {
		if selection.SessionID == sessionID {
			delete(h.microsoftSelections, selectionID)
			selection.canceled.Store(true)
			if selection.timer != nil {
				selection.timer.Stop()
				selection.timer = nil
			}
			removed = append(removed, selection)
		}
	}
	h.microsoftSelectionsMu.Unlock()
	for _, selection := range removed {
		discardDetachedMicrosoftProfileSelection(selection)
	}
}

func discardDetachedMicrosoftProfileSelection(selection *microsoftProfileSelection) {
	selection.mu.Lock()
	selection.Account = config.Account{}
	selection.Profiles = nil
	selection.mu.Unlock()
}

func discardDetachedMicrosoftProfileSelections(selections []*microsoftProfileSelection) {
	for _, selection := range selections {
		discardDetachedMicrosoftProfileSelection(selection)
	}
}

func clearMicrosoftAccountCredential(account *config.Account) {
	account.AccessToken = ""
	account.RefreshToken = ""
	account.ClientSecret = ""
}

func (h *Handler) markMicrosoftSessionCanceled(sessionID string) {
	now := time.Now()
	h.microsoftFlowMu.Lock()
	if h.microsoftCanceled == nil {
		h.microsoftCanceled = make(map[string]time.Time)
	}
	h.cleanupMicrosoftCanceledLocked(now)
	if len(h.microsoftCanceled) >= microsoftMaxCanceledSessionTombstones {
		var oldestID string
		var oldestExpiry time.Time
		for id, expiry := range h.microsoftCanceled {
			if oldestID == "" || expiry.Before(oldestExpiry) {
				oldestID = id
				oldestExpiry = expiry
			}
		}
		delete(h.microsoftCanceled, oldestID)
	}
	h.microsoftCanceled[sessionID] = now.Add(microsoftCanceledSessionTTL)
	if discovery := h.microsoftDiscoveries[sessionID]; discovery != nil {
		discovery.cancel()
	}
	h.microsoftFlowMu.Unlock()
}

func (h *Handler) beginMicrosoftProfileDiscovery(
	parent context.Context,
	sessionID string,
) (context.Context, *microsoftProfileDiscovery, bool) {
	sessionID = strings.TrimSpace(sessionID)
	h.microsoftFlowMu.Lock()
	defer h.microsoftFlowMu.Unlock()
	if h.microsoftSessionCanceledLocked(sessionID, time.Now()) {
		return nil, nil, false
	}
	if h.microsoftDiscoveries == nil {
		h.microsoftDiscoveries = make(map[string]*microsoftProfileDiscovery)
	}
	if h.microsoftDiscoveries[sessionID] != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(parent, microsoftProfileDiscoveryTimeout)
	discovery := &microsoftProfileDiscovery{cancel: cancel}
	h.microsoftDiscoveries[sessionID] = discovery
	return ctx, discovery, true
}

func (h *Handler) endMicrosoftProfileDiscovery(sessionID string, expected *microsoftProfileDiscovery) {
	expected.cancel()
	h.microsoftFlowMu.Lock()
	if h.microsoftDiscoveries[strings.TrimSpace(sessionID)] == expected {
		delete(h.microsoftDiscoveries, strings.TrimSpace(sessionID))
	}
	h.microsoftFlowMu.Unlock()
}

func (h *Handler) microsoftSessionCanceled(sessionID string) bool {
	h.microsoftFlowMu.Lock()
	defer h.microsoftFlowMu.Unlock()
	return h.microsoftSessionCanceledLocked(sessionID, time.Now())
}

func (h *Handler) microsoftSessionCanceledLocked(sessionID string, now time.Time) bool {
	h.cleanupMicrosoftCanceledLocked(now)
	expiry, exists := h.microsoftCanceled[strings.TrimSpace(sessionID)]
	return exists && now.Before(expiry)
}

func (h *Handler) cleanupMicrosoftCanceledLocked(now time.Time) {
	for sessionID, expiry := range h.microsoftCanceled {
		if !now.Before(expiry) {
			delete(h.microsoftCanceled, sessionID)
		}
	}
}

func (h *Handler) writeMicrosoftSSOCanceled(w http.ResponseWriter) {
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]string{"error": "Microsoft SSO login was canceled"})
}

func (h *Handler) writeAddAccountError(w http.ResponseWriter, err error, rotatedRefreshToken ...string) {
	if errors.Is(err, config.ErrDuplicateAccountID) ||
		errors.Is(err, config.ErrDuplicateRefreshToken) ||
		errors.Is(err, config.ErrDuplicateAPIKey) {
		w.WriteHeader(http.StatusConflict)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	payload := map[string]string{"error": err.Error()}
	if len(rotatedRefreshToken) > 0 {
		if rotated := strings.TrimSpace(rotatedRefreshToken[0]); rotated != "" {
			// Microsoft may have already invalidated the original refresh token.
			// Surface the rotated value so operators can retry import without a
			// full interactive re-login.
			payload["rotatedRefreshToken"] = rotated
			payload["hint"] = "The identity provider rotated the refresh token before persistence failed; retry import with rotatedRefreshToken"
		}
	}
	json.NewEncoder(w).Encode(payload)
}

func (h *Handler) apiStartBuilderIdLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Region string `json:"region"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	session, err := auth.StartBuilderIdLogin(req.Region)
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId":       session.ID,
		"userCode":        session.UserCode,
		"verificationUri": session.VerificationUri,
		"interval":        session.Interval,
	})
}

func (h *Handler) apiPollBuilderIdAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	accessToken, refreshToken, clientID, clientSecret, region, expiresIn, status, err := auth.PollBuilderIdAuth(req.SessionID)
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	if status == "pending" || status == "slow_down" {
		// 获取当前间隔
		interval := 5
		if session := auth.GetBuilderIdSession(req.SessionID); session != nil {
			interval = session.Interval
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"completed": false,
			"status":    status,
			"interval":  interval,
		})
		return
	}

	// 授权完成，获取用户信息
	email, _, _ := auth.GetUserInfo(accessToken)

	// 创建账号
	account := config.Account{
		ID:           auth.GenerateAccountID(),
		Email:        email,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthMethod:   "idc",
		Provider:     "BuilderId",
		Region:       region,
		ExpiresAt:    time.Now().Unix() + int64(expiresIn),
		Enabled:      true,
		MachineId:    config.GenerateMachineId(),
	}

	if err := config.AddAccount(account); err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	h.pool.Reload()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"completed": true,
		"account": map[string]interface{}{
			"id":    account.ID,
			"email": account.Email,
		},
	})
}

func (h *Handler) apiImportSsoToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BearerToken string `json:"bearerToken"`
		Region      string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	if req.BearerToken == "" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "bearerToken is required"})
		return
	}

	// 支持批量导入，按行分割
	tokens := strings.Split(strings.TrimSpace(req.BearerToken), "\n")
	var imported []map[string]interface{}
	var errors []string

	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		accessToken, refreshToken, clientID, clientSecret, expiresIn, err := auth.ImportFromSsoToken(token, req.Region)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}

		// 获取用户信息
		email, _, _ := auth.GetUserInfo(accessToken)

		// 创建账号
		account := config.Account{
			ID:           auth.GenerateAccountID(),
			Email:        email,
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthMethod:   "idc",
			Region:       req.Region,
			ExpiresAt:    time.Now().Unix() + int64(expiresIn),
			Enabled:      true,
			MachineId:    config.GenerateMachineId(),
		}

		if err := config.AddAccount(account); err != nil {
			errors = append(errors, err.Error())
			continue
		}

		imported = append(imported, map[string]interface{}{
			"id":    account.ID,
			"email": account.Email,
		})
	}

	h.pool.Reload()

	if len(imported) == 0 && len(errors) > 0 {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   strings.Join(errors, "; "),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"accounts": imported,
		"errors":   errors,
	})
}

func (h *Handler) apiImportCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		UserID        string `json:"userId"`
		Nickname      string `json:"nickname"`
		ProfileARN    string `json:"profileArn"`
		AccessToken   string `json:"accessToken"`
		RefreshToken  string `json:"refreshToken"`
		KiroApiKey    string `json:"kiroApiKey"`
		ClientID      string `json:"clientId"`
		ClientSecret  string `json:"clientSecret"`
		AuthMethod    string `json:"authMethod"`
		Provider      string `json:"provider"`
		Region        string `json:"region"`
		TokenEndpoint string `json:"tokenEndpoint"`
		IssuerURL     string `json:"issuerUrl"`
		Scopes        string `json:"scopes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	req.KiroApiKey = strings.TrimSpace(req.KiroApiKey)
	req.AccessToken = strings.TrimSpace(req.AccessToken)
	methodHint := strings.ToLower(strings.TrimSpace(req.AuthMethod))
	isAPIKeyImport := req.KiroApiKey != "" ||
		methodHint == "api_key" || methodHint == "apikey" ||
		(req.RefreshToken == "" && looksLikeKiroAPIKey(req.AccessToken))
	if isAPIKeyImport && req.KiroApiKey == "" {
		// Allow plain-text / AccessToken-only API key imports.
		req.KiroApiKey = req.AccessToken
	}
	if !isAPIKeyImport && req.RefreshToken == "" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "refreshToken or kiroApiKey is required"})
		return
	}
	if len(req.RefreshToken) > 512<<10 || len(req.AccessToken) > 512<<10 || len(req.KiroApiKey) > 512<<10 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "credential token is too long"})
		return
	}
	originalRefreshToken := req.RefreshToken
	h.credentialImportMu.Lock()
	defer h.credentialImportMu.Unlock()
	accountID := strings.TrimSpace(req.ID)
	if accountID != "" {
		if _, err := uuid.Parse(accountID); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "id must be a UUID"})
			return
		}
		if config.AccountIDExists(accountID) {
			h.writeAddAccountError(w, config.ErrDuplicateAccountID)
			return
		}
	}

	// API Key import path: no OAuth refresh, no profileArn.
	if isAPIKeyImport {
		if accountID == "" {
			accountID = auth.GenerateAccountID()
		}
		account := config.Account{
			ID:         accountID,
			Email:      strings.TrimSpace(req.Email),
			UserId:     strings.TrimSpace(req.UserID),
			Nickname:   strings.TrimSpace(req.Nickname),
			KiroApiKey: req.KiroApiKey,
			AuthMethod: "api_key",
			Provider:   strings.TrimSpace(req.Provider),
			Region:     strings.TrimSpace(req.Region),
			Enabled:    true,
		}
		if err := config.NormalizeAPIKeyAccount(&account); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if config.AccountAPIKeyExists(account.KiroApiKey) {
			h.writeAddAccountError(w, config.ErrDuplicateAPIKey)
			return
		}
		if err := config.AddAccount(account); err != nil {
			h.writeAddAccountError(w, err)
			return
		}
		h.pool.Reload()
		if account.Enabled && account.AccessToken != "" {
			go func(acc config.Account) {
				if err := h.fetchAndCacheAccountModels(&acc); err != nil {
					logger.Warnf("[ModelsCache] Auto-refresh failed for new API key account %s: %v", acc.Email, err)
				}
			}(account)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"account": map[string]interface{}{
				"id":    account.ID,
				"email": account.Email,
			},
		})
		return
	}

	if config.AccountCredentialExists(originalRefreshToken) {
		h.writeAddAccountError(w, config.ErrDuplicateRefreshToken)
		return
	}

	// 设置默认值
	req.Region = strings.TrimSpace(req.Region)
	if req.Region == "" {
		req.Region = "us-east-1"
	}
	method := strings.ToLower(strings.TrimSpace(req.AuthMethod))
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	derivedTokenEndpoint, derivedIssuer, derivedScopes := auth.DeriveExternalIdpEndpoints(
		req.UserID, req.ClientID, req.AccessToken,
	)
	// Explicit AWS/social methods win over incidental tokenEndpoint/issuerUrl
	// fields so mixed JSON templates cannot force the external-IdP path.
	explicitMicrosoft := method == "external_idp" || method == "external-idp" ||
		method == "external" || method == "microsoft" || method == "m365" || method == "office365" ||
		method == "azure" || method == "azuread" || method == "azure-ad" || method == "azure_ad" ||
		method == "entra" || method == "entra-id" ||
		provider == "external" || provider == "microsoft" || provider == "m365" || provider == "office365" ||
		provider == "azure" || provider == "azuread" || provider == "azure-ad" || provider == "azure_ad" ||
		provider == "entra" || provider == "entra-id"
	implicitExternal := method == "" && provider == "" &&
		(derivedTokenEndpoint != "" ||
			strings.TrimSpace(req.TokenEndpoint) != "" ||
			strings.TrimSpace(req.IssuerURL) != "")
	switch {
	case method == "api_key" || method == "apikey":
		req.AuthMethod = "api_key"
	case method == "idc" || method == "builderid" || method == "enterprise":
		req.AuthMethod = "idc"
	case method == "social" || method == "google" || method == "github":
		req.AuthMethod = "social"
	case explicitMicrosoft || implicitExternal:
		req.AuthMethod = auth.MicrosoftSSOAuthMethod
	case req.ClientID != "" && req.ClientSecret != "":
		req.AuthMethod = "idc"
	default:
		req.AuthMethod = "social"
	}

	// 用 refreshToken 刷新获取新的 accessToken。导入必须以一次成功的刷新为前提：
	// 本地缓存里的 accessToken 不携带可信的过期时间，盲猜短 TTL 会让账号在选号时
	// 永远被跳过，导致后台/按需刷新都无法触发（详见 ensureValidToken 与 Pick 的过期判定）。
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.TokenEndpoint = strings.TrimSpace(req.TokenEndpoint)
	req.IssuerURL = strings.TrimRight(strings.TrimSpace(req.IssuerURL), "/")
	req.Scopes = strings.TrimSpace(req.Scopes)

	if req.AuthMethod == auth.MicrosoftSSOAuthMethod {
		if req.IssuerURL == "" {
			req.IssuerURL = derivedIssuer
		}
		if req.IssuerURL == "" && req.TokenEndpoint != "" {
			normalizedTokenEndpoint, tokenIssuer, tokenScopes := auth.ExternalIdpConfigurationFromTokenEndpoint(
				req.TokenEndpoint, req.ClientID,
			)
			if normalizedTokenEndpoint != "" {
				req.TokenEndpoint = normalizedTokenEndpoint
				req.IssuerURL = tokenIssuer
				if req.Scopes == "" {
					req.Scopes = tokenScopes
				}
			}
		}
		if req.IssuerURL != "" {
			builtTokenEndpoint, normalizedIssuer, builtScopes := auth.ExternalIdpConfigurationFromIssuer(req.IssuerURL, req.ClientID)
			if req.TokenEndpoint == "" {
				req.TokenEndpoint = builtTokenEndpoint
			}
			if normalizedIssuer != "" {
				req.IssuerURL = normalizedIssuer
			}
			if req.Scopes == "" {
				req.Scopes = builtScopes
			}
		}
		if req.TokenEndpoint == "" {
			req.TokenEndpoint = derivedTokenEndpoint
		}
		if req.Scopes == "" {
			req.Scopes = derivedScopes
		}
		normalizedScopes, err := auth.NormalizeExternalIdpScopes(req.Scopes, req.ClientID)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		req.Scopes = normalizedScopes
		if err := auth.ValidateExternalIdpConfiguration(req.ClientID, req.TokenEndpoint, req.IssuerURL, req.Scopes); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		req.Provider = auth.MicrosoftSSOProvider
		req.ClientSecret = ""
	}

	profileARN := strings.TrimSpace(req.ProfileARN)
	if profileARN != "" {
		canonicalARN, _, ok := parseKiroProfileArn(profileARN)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "profileArn is invalid"})
			return
		}
		profileARN = canonicalARN
	}

	tempAccount := &config.Account{
		RefreshToken:  req.RefreshToken,
		ClientID:      req.ClientID,
		ClientSecret:  req.ClientSecret,
		AuthMethod:    req.AuthMethod,
		Region:        req.Region,
		TokenEndpoint: req.TokenEndpoint,
		IssuerURL:     req.IssuerURL,
		Scopes:        req.Scopes,
	}
	accessToken, newRefreshToken, expiresAt, newProfileArn, err := auth.RefreshToken(tempAccount)
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "Token refresh failed: " + err.Error()})
		return
	}
	if newRefreshToken != "" {
		req.RefreshToken = newRefreshToken
	}
	rotatedRefreshToken := ""
	if req.RefreshToken != "" && req.RefreshToken != originalRefreshToken {
		rotatedRefreshToken = req.RefreshToken
	}

	// 获取用户信息
	email := strings.TrimSpace(req.Email)
	userID := strings.TrimSpace(req.UserID)
	if req.AuthMethod == auth.MicrosoftSSOAuthMethod {
		tokenEmail, tokenUserID := auth.ExternalIdpTokenIdentity(accessToken)
		if tokenEmail != "" {
			email = tokenEmail
		}
		if tokenUserID != "" {
			userID = tokenUserID
		}
	} else if tokenEmail, _, _ := auth.GetUserInfo(accessToken); tokenEmail != "" {
		email = tokenEmail
	}

	if accountID == "" {
		accountID = auth.GenerateAccountID()
	}
	if profileARN == "" {
		profileARN = newProfileArn
	}
	// Interactive Microsoft login only accepts profiles discovered for the
	// refreshed token. Import keeps the same trust boundary so a client cannot
	// pin an arbitrary data-plane ARN onto a working credential.
	if req.AuthMethod == auth.MicrosoftSSOAuthMethod && profileARN != "" {
		probeAccount := *tempAccount
		probeAccount.AccessToken = accessToken
		probeAccount.RefreshToken = req.RefreshToken
		probeAccount.ExpiresAt = expiresAt
		offeredProfiles, discoverErr := DiscoverKiroProfiles(&probeAccount)
		if discoverErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Unable to verify profileArn against Kiro profiles: " + discoverErr.Error(),
			})
			return
		}
		offered := false
		for _, profile := range offeredProfiles {
			if profile.ARN == profileARN {
				offered = true
				break
			}
		}
		if !offered {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "profileArn was not offered for this credential",
			})
			return
		}
	}

	// 创建账号
	account := config.Account{
		ID:                      accountID,
		Email:                   email,
		UserId:                  userID,
		Nickname:                strings.TrimSpace(req.Nickname),
		AccessToken:             accessToken,
		RefreshToken:            req.RefreshToken,
		RefreshTokenFingerprint: config.RefreshTokenFingerprint(originalRefreshToken),
		ClientID:                req.ClientID,
		ClientSecret:            req.ClientSecret,
		AuthMethod:              req.AuthMethod,
		Provider:                req.Provider,
		Region:                  req.Region,
		ExpiresAt:               expiresAt,
		Enabled:                 true,
		MachineId:               config.GenerateMachineId(),
		ProfileArn:              profileARN,
		TokenEndpoint:           req.TokenEndpoint,
		IssuerURL:               req.IssuerURL,
		Scopes:                  req.Scopes,
	}

	if err := config.AddAccount(account); err != nil {
		h.writeAddAccountError(w, err, rotatedRefreshToken)
		return
	}

	h.pool.Reload()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"account": map[string]interface{}{
			"id":    account.ID,
			"email": account.Email,
		},
	})
}
