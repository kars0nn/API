package middleware

import (
	"strings"
	"time"

	"github.com/seventv/api/data/query"
	"github.com/seventv/api/internal/global"
	"github.com/seventv/common/auth"
	"github.com/seventv/common/errors"
	"github.com/seventv/common/structures/v3"
	"github.com/seventv/common/utils"
	"github.com/valyala/fasthttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func Auth(gCtx global.Context) Middleware {
	return func(ctx *fasthttp.RequestCtx) errors.APIError {
		// Parse token from header
		h := utils.B2S(ctx.Request.Header.Peek("Authorization"))
		if len(h) == 0 {
			return nil
		}

		s := strings.Split(h, "Bearer ")
		if len(s) != 2 {
			return errors.ErrUnauthorized().SetDetail("Bad Authorization Header")
		}

		t := s[1]

		user, err := DoAuth(gCtx, t)

		ctx.SetUserValue("user", user)

		if err != nil {
			return err
		}

		return nil
	}
}

func DoAuth(ctx global.Context, t string) (structures.User, errors.APIError) {
	// Verify the token
	claims := &auth.JWTClaimUser{}

	user := structures.User{}

	_, err := auth.VerifyJWT(ctx.Config().Credentials.JWTSecret, strings.Split(t, "."), claims)
	if err != nil {
		return user, errors.ErrUnauthorized().SetDetail(err.Error())
	}

	// User ID from parsed token
	if claims.UserID == "" {
		return user, errors.ErrUnauthorized().SetDetail("Bad Token")
	}

	userID, err := primitive.ObjectIDFromHex(claims.UserID)
	if err != nil {
		return user, errors.ErrUnauthorized().SetDetail(err.Error())
	}

	user, err = ctx.Inst().Query.Users(ctx, bson.M{"_id": userID}).First()
	if err != nil {
		return user, errors.From(err)
	}

	if user.TokenVersion != claims.TokenVersion {
		return user, errors.ErrUnauthorized().SetDetail("Token Version Mismatch")
	}

	// Check bans
	bans, err := ctx.Inst().Query.Bans(ctx, query.BanQueryOptions{
		Filter: bson.M{"effects": bson.M{"$bitsAnySet": structures.BanEffectNoAuth | structures.BanEffectNoPermissions}},
	})
	if err != nil {
		return user, errors.ErrInternalServerError().SetDetail("Failed")
	}

	if _, noRights := bans.NoPermissions[userID]; noRights {
		user.Roles = []structures.Role{structures.RevocationRole}
	}

	if ban, noAuth := bans.NoAuth[userID]; noAuth {
		user.Bans = append(user.Bans, ban)

		return user, errors.ErrBanned().SetDetail(ban.Reason).SetFields(errors.Fields{
			"ban": map[string]string{
				"reason":    ban.Reason,
				"expire_at": ban.ExpireAt.Format(time.RFC3339),
			},
		})
	}

	return user, nil
}
