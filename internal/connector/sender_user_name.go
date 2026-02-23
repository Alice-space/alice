package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

var (
	userNameCache       sync.Map
	chatMemberNameCache sync.Map
)

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

func (s *LarkSender) ResolveChatMemberName(ctx context.Context, chatID, openID, userID string) (string, error) {
	chatID = strings.TrimSpace(chatID)
	candidates := []struct {
		idType string
		id     string
	}{
		{idType: larkim.MemberIdTypeGetChatMembersOpenId, id: strings.TrimSpace(openID)},
		{idType: larkim.MemberIdTypeGetChatMembersUserId, id: strings.TrimSpace(userID)},
	}

	var lastErr error
	for _, candidate := range candidates {
		if chatID == "" || candidate.id == "" {
			continue
		}
		cacheKey := chatMemberCacheKey(chatID, candidate.idType, candidate.id)
		if cachedName, ok := chatMemberNameCache.Load(cacheKey); ok {
			if name, ok := cachedName.(string); ok && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name), nil
			}
		}

		name, err := s.fetchChatMemberName(ctx, chatID, candidate.idType, candidate.id)
		if err != nil {
			lastErr = err
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		chatMemberNameCache.Store(cacheKey, name)
		return name, nil
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("empty chat member id")
}

func (s *LarkSender) fetchChatMemberName(ctx context.Context, chatID, memberIDType, memberID string) (string, error) {
	pageToken := ""
	for {
		reqBuilder := larkim.NewGetChatMembersReqBuilder().
			ChatId(chatID).
			MemberIdType(memberIDType).
			PageSize(100)
		if pageToken != "" {
			reqBuilder.PageToken(pageToken)
		}

		resp, err := s.client.Im.V1.ChatMembers.Get(ctx, reqBuilder.Build())
		if err != nil {
			return "", err
		}
		if !resp.Success() {
			return "", fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
		}
		if resp.Data == nil {
			return "", errors.New("get chat members success but data is empty")
		}

		for _, item := range resp.Data.Items {
			if item == nil {
				continue
			}
			candidateID := strings.TrimSpace(deref(item.MemberId))
			candidateName := strings.TrimSpace(deref(item.Name))
			if candidateID == "" || candidateName == "" {
				continue
			}
			chatMemberNameCache.Store(
				chatMemberCacheKey(chatID, memberIDType, candidateID),
				candidateName,
			)
		}
		if cachedName, ok := chatMemberNameCache.Load(chatMemberCacheKey(chatID, memberIDType, memberID)); ok {
			if name, ok := cachedName.(string); ok && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name), nil
			}
		}

		hasMore := resp.Data.HasMore != nil && *resp.Data.HasMore
		if !hasMore {
			break
		}
		pageToken = strings.TrimSpace(deref(resp.Data.PageToken))
		if pageToken == "" {
			break
		}
	}
	return "", errors.New("chat member not found")
}

func chatMemberCacheKey(chatID, memberIDType, memberID string) string {
	return strings.TrimSpace(chatID) + "|" + strings.TrimSpace(memberIDType) + ":" + strings.TrimSpace(memberID)
}

var _ ChatMemberNameResolver = (*LarkSender)(nil)
