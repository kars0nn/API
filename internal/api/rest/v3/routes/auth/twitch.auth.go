package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/seventv/api/internal/api/rest/middleware"
	"github.com/seventv/api/internal/api/rest/rest"
	"github.com/seventv/api/internal/global"
	"github.com/seventv/common/auth"
	"github.com/seventv/common/errors"
	"github.com/seventv/common/utils"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

const TWITCH_CSRF_COOKIE_NAME = "csrf_token_tw"

type OAuth2CallbackAppParams struct {
	Token string `url:"token"`
}

var twitchScopes = []string{
	"user:read:email",
}

type twitch struct {
	Ctx global.Context
}

func newTwitch(gCtx global.Context) rest.Route {
	return &twitch{gCtx}
}

func (r *twitch) Config() rest.RouteConfig {
	return rest.RouteConfig{
		URI:      "/twitch",
		Method:   rest.GET,
		Children: []rest.Route{},
		Middleware: []rest.Middleware{
			// Handle binding token
			// this is for linking the connection to an existing account
			bindMiddleware,
			middleware.Auth(r.Ctx, false)},
	}
}

func (r *twitch) Handler(ctx *rest.Ctx) rest.APIError {
	actor, _ := ctx.GetActor()

	// Generate a randomized value for a CSRF token
	csrfValue, err := utils.GenerateRandomString(64)
	if err != nil {
		zap.S().Errorw("csrf, random bytes",
			"error", err,
		)

		return errors.ErrInternalServerError()
	}

	// Sign a JWT with the CSRF bytes
	csrfToken, err := auth.SignJWT(r.Ctx.Config().Credentials.JWTSecret, auth.JWTClaimOAuth2CSRF{
		State:       csrfValue,
		CreatedAt:   time.Now(),
		Bind:        actor.ID,
		OldRedirect: ctx.QueryArgs().GetBool("old"),
	})
	if err != nil {
		ctx.Log().Errorw("csrf, jwt",
			"error", err,
		)

		return errors.ErrInternalServerError()
	}

	// Set cookie
	cookie := fasthttp.Cookie{}
	cookie.SetKey(TWITCH_CSRF_COOKIE_NAME)
	cookie.SetValue(csrfToken)
	cookie.SetExpire(time.Now().Add(time.Minute * 5))
	cookie.SetHTTPOnly(true)
	cookie.SetDomain(r.Ctx.Config().Http.Cookie.Domain)
	cookie.SetSecure(r.Ctx.Config().Http.Cookie.Secure)
	ctx.Response.Header.SetCookie(&cookie)

	// Format querystring options for the redirection URL
	params, err := query.Values(&OAuth2URLParams{
		ClientID:     r.Ctx.Config().Platforms.Twitch.ClientID,
		RedirectURI:  r.Ctx.Config().Platforms.Twitch.RedirectURI,
		ResponseType: "code",
		Scope:        strings.Join(twitchScopes, " "),
		State:        csrfValue,
	})
	if err != nil {
		zap.S().Errorw("querystring",
			"error", err,
		)

		return errors.ErrInternalServerError()
	}

	// Redirect the client
	ctx.Redirect(fmt.Sprintf("https://id.twitch.tv/oauth2/authorize?%s", params.Encode()), int(rest.Found))

	return nil
}
