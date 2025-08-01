// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package auth

import (
	"context"
	"errors"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/mcache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/dbbuild"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	redis2 "github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	pbauth "github.com/openimsdk/protocol/auth"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msggateway"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/tokenverify"
	"google.golang.org/grpc"
)

type authServer struct {
	pbauth.UnimplementedAuthServer
	authDatabase   controller.AuthDatabase
	RegisterCenter discovery.Conn
	config         *Config
	userClient     *rpcli.UserClient
	adminUserIDs   []string
}

type Config struct {
	RpcConfig   config.Auth
	RedisConfig config.Redis
	MongoConfig config.Mongo
	Share       config.Share
	Discovery   config.Discovery
}

func Start(ctx context.Context, config *Config, client discovery.SvcDiscoveryRegistry, server grpc.ServiceRegistrar) error {
	dbb := dbbuild.NewBuilder(&config.MongoConfig, &config.RedisConfig)
	rdb, err := dbb.Redis(ctx)
	if err != nil {
		return err
	}
	var token cache.TokenModel
	if rdb == nil {
		mdb, err := dbb.Mongo(ctx)
		if err != nil {
			return err
		}
		mc, err := mgo.NewCacheMgo(mdb.GetDB())
		if err != nil {
			return err
		}
		token = mcache.NewTokenCacheModel(mc, config.RpcConfig.TokenPolicy.Expire)
	} else {
		token = redis2.NewTokenCacheModel(rdb, config.RpcConfig.TokenPolicy.Expire)
	}
	userConn, err := client.GetConn(ctx, config.Discovery.RpcService.User)
	if err != nil {
		return err
	}
	pbauth.RegisterAuthServer(server, &authServer{
		RegisterCenter: client,
		authDatabase: controller.NewAuthDatabase(
			token,
			config.Share.Secret,
			config.RpcConfig.TokenPolicy.Expire,
			config.Share.MultiLogin,
			config.Share.IMAdminUser.UserIDs,
		),
		config:       config,
		userClient:   rpcli.NewUserClient(userConn),
		adminUserIDs: config.Share.IMAdminUser.UserIDs,
	})
	return nil
}

func (s *authServer) GetAdminToken(ctx context.Context, req *pbauth.GetAdminTokenReq) (*pbauth.GetAdminTokenResp, error) {
	resp := pbauth.GetAdminTokenResp{}
	if req.Secret != s.config.Share.Secret {
		return nil, errs.ErrNoPermission.WrapMsg("secret invalid")
	}

	if !datautil.Contain(req.UserID, s.adminUserIDs...) {
		return nil, errs.ErrArgs.WrapMsg("userID is error.", "userID", req.UserID, "adminUserID", s.adminUserIDs)

	}

	if err := s.userClient.CheckUser(ctx, []string{req.UserID}); err != nil {
		return nil, err
	}

	token, err := s.authDatabase.CreateToken(ctx, req.UserID, int(constant.AdminPlatformID))
	if err != nil {
		return nil, err
	}

	prommetrics.UserLoginCounter.Inc()
	resp.Token = token
	resp.ExpireTimeSeconds = s.config.RpcConfig.TokenPolicy.Expire * 24 * 60 * 60
	return &resp, nil
}

func (s *authServer) GetUserToken(ctx context.Context, req *pbauth.GetUserTokenReq) (*pbauth.GetUserTokenResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}

	if req.PlatformID == constant.AdminPlatformID {
		return nil, errs.ErrNoPermission.WrapMsg("platformID invalid. platformID must not be adminPlatformID")
	}

	resp := pbauth.GetUserTokenResp{}

	if authverify.CheckUserIsAdmin(ctx, req.UserID) {
		return nil, errs.ErrNoPermission.WrapMsg("don't get Admin token")
	}
	user, err := s.userClient.GetUserInfo(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if user.AppMangerLevel >= constant.AppNotificationAdmin {
		return nil, errs.ErrArgs.WrapMsg("app account can`t get token")
	}
	token, err := s.authDatabase.CreateToken(ctx, req.UserID, int(req.PlatformID))
	if err != nil {
		return nil, err
	}
	resp.Token = token
	resp.ExpireTimeSeconds = s.config.RpcConfig.TokenPolicy.Expire * 24 * 60 * 60
	return &resp, nil
}

func (s *authServer) parseToken(ctx context.Context, tokensString string) (claims *tokenverify.Claims, err error) {
	claims, err = tokenverify.GetClaimFromToken(tokensString, authverify.Secret(s.config.Share.Secret))
	if err != nil {
		return nil, err
	}
	m, err := s.authDatabase.GetTokensWithoutError(ctx, claims.UserID, claims.PlatformID)
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		isAdmin := authverify.CheckUserIsAdmin(ctx, claims.UserID)
		if isAdmin {
			if err = s.authDatabase.GetTemporaryTokensWithoutError(ctx, claims.UserID, claims.PlatformID, tokensString); err == nil {
				return claims, nil
			}
		}
		return nil, servererrs.ErrTokenNotExist.Wrap()
	}
	if v, ok := m[tokensString]; ok {
		switch v {
		case constant.NormalToken:
			return claims, nil
		case constant.KickedToken:
			return nil, servererrs.ErrTokenKicked.Wrap()
		default:
			return nil, errs.Wrap(errs.ErrTokenUnknown)
		}
	} else {
		isAdmin := authverify.CheckUserIsAdmin(ctx, claims.UserID)
		if isAdmin {
			if err = s.authDatabase.GetTemporaryTokensWithoutError(ctx, claims.UserID, claims.PlatformID, tokensString); err == nil {
				return claims, nil
			}
		}
	}
	return nil, servererrs.ErrTokenNotExist.Wrap()
}

func (s *authServer) ParseToken(ctx context.Context, req *pbauth.ParseTokenReq) (resp *pbauth.ParseTokenResp, err error) {
	resp = &pbauth.ParseTokenResp{}
	claims, err := s.parseToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	resp.UserID = claims.UserID
	resp.PlatformID = int32(claims.PlatformID)
	resp.ExpireTimeSeconds = claims.ExpiresAt.Unix()
	return resp, nil
}

func (s *authServer) ForceLogout(ctx context.Context, req *pbauth.ForceLogoutReq) (*pbauth.ForceLogoutResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	if err := s.forceKickOff(ctx, req.UserID, req.PlatformID); err != nil {
		return nil, err
	}
	return &pbauth.ForceLogoutResp{}, nil
}

func (s *authServer) forceKickOff(ctx context.Context, userID string, platformID int32) error {
	conns, err := s.RegisterCenter.GetConns(ctx, s.config.Discovery.RpcService.MessageGateway)
	if err != nil {
		return err
	}
	for _, v := range conns {
		log.ZDebug(ctx, "forceKickOff", "userID", userID, "platformID", platformID)
		client := msggateway.NewMsgGatewayClient(v)
		kickReq := &msggateway.KickUserOfflineReq{KickUserIDList: []string{userID}, PlatformID: platformID}
		_, err := client.KickUserOffline(ctx, kickReq)
		if err != nil {
			log.ZError(ctx, "forceKickOff", err, "kickReq", kickReq)
		}
	}

	m, err := s.authDatabase.GetTokensWithoutError(ctx, userID, int(platformID))
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	for k := range m {
		m[k] = constant.KickedToken
		log.ZDebug(ctx, "set token map is ", "token map", m, "userID",
			userID, "token", k)

		err = s.authDatabase.SetTokenMapByUidPid(ctx, userID, int(platformID), m)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *authServer) InvalidateToken(ctx context.Context, req *pbauth.InvalidateTokenReq) (*pbauth.InvalidateTokenResp, error) {
	m, err := s.authDatabase.GetTokensWithoutError(ctx, req.UserID, int(req.PlatformID))
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if m == nil {
		return nil, errs.New("token map is empty").Wrap()
	}
	log.ZDebug(ctx, "get token from redis", "userID", req.UserID, "platformID",
		req.PlatformID, "tokenMap", m)

	for k := range m {
		if k != req.GetPreservedToken() {
			m[k] = constant.KickedToken
		}
	}
	log.ZDebug(ctx, "set token map is ", "token map", m, "userID",
		req.UserID, "token", req.GetPreservedToken())
	err = s.authDatabase.SetTokenMapByUidPid(ctx, req.UserID, int(req.PlatformID), m)
	if err != nil {
		return nil, err
	}
	return &pbauth.InvalidateTokenResp{}, nil
}

func (s *authServer) KickTokens(ctx context.Context, req *pbauth.KickTokensReq) (*pbauth.KickTokensResp, error) {
	if err := s.authDatabase.BatchSetTokenMapByUidPid(ctx, req.Tokens); err != nil {
		return nil, err
	}
	return &pbauth.KickTokensResp{}, nil
}
