package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
)

var userNameCache sync.Map

func (s *LarkSender) ResolveUserName(ctx context.Context, openID, userID string) (string, error) {
	candidates := []struct {
		idType string
		id     string
	}{
		{idType: larkcontact.UserIdTypeGetUserOpenId, id: strings.TrimSpace(openID)},
		{idType: larkcontact.UserIdTypeGetUserUserId, id: strings.TrimSpace(userID)},
	}

	var lastErr error
	for _, candidate := range candidates {
		if candidate.id == "" {
			continue
		}
		cacheKey := candidate.idType + ":" + candidate.id
		if cachedName, ok := userNameCache.Load(cacheKey); ok {
			if name, ok := cachedName.(string); ok && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name), nil
			}
		}

		name, err := s.fetchUserName(ctx, candidate.idType, candidate.id)
		if err != nil {
			lastErr = err
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		userNameCache.Store(cacheKey, name)
		return name, nil
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("empty user id")
}

func (s *LarkSender) fetchUserName(ctx context.Context, idType, id string) (string, error) {
	req := larkcontact.NewGetUserReqBuilder().
		UserId(id).
		UserIdType(idType).
		Build()

	resp, err := s.client.Contact.V3.User.Get(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	if resp.Data == nil || resp.Data.User == nil {
		return "", errors.New("get user success but user is empty")
	}
	return strings.TrimSpace(deref(resp.Data.User.Name)), nil
}

var _ UserNameResolver = (*LarkSender)(nil)
