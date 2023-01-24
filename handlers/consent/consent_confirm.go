package consent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	client "github.com/ory/hydra-client-go/v2"
	"misso/consts"
	"misso/global"
	"misso/types"
	"net/http"
	"time"
)

type ConsentConfirmRequest struct {
	CSRF     string `form:"_csrf"`
	Remember bool   `form:"remember"`
	Action   string `form:"action"`
}

func ConsentConfirm(ctx *gin.Context) {

	// Parse request
	global.Logger.Debugf("Parsing request...")
	var req ConsentConfirmRequest
	err := ctx.Bind(&req)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.tmpl", gin.H{
			"error": "Failed to parse request",
		})
		return
	}

	// Validate CSRF
	global.Logger.Debugf("Validating CSRF...")
	sessKey := fmt.Sprintf(consts.REDIS_KEY_CONSENT_CSRF, req.CSRF)
	oauth2challenge, err := global.Redis.Get(context.Background(), sessKey).Result()
	if err != nil {
		global.Logger.Errorf("Failed to get csrf from redis with error: %v", err)
		ctx.HTML(http.StatusInternalServerError, "error.tmpl", gin.H{
			"error": "Failed to get csrf",
		})
		return
	}

	// Delete used challenge
	global.Redis.Del(context.Background(), sessKey)

	// Get OAuth2 Client
	global.Logger.Debugf("Getting OAuth2 Consent Request...")
	consentReq, _, err := global.Hydra.Admin.OAuth2Api.GetOAuth2ConsentRequest(context.Background()).ConsentChallenge(oauth2challenge).Execute()
	if err != nil {
		global.Logger.Errorf("Failed to get required consent request with error: %v", err)
		ctx.HTML(http.StatusInternalServerError, "error.tmpl", gin.H{
			"error": "Failed to get required consent request",
		})
		return
	}

	// Check action type
	if req.Action == "reject" {
		global.Logger.Debugf("User rejected the request, reporting back to hydra...")
		errId := "rejected_by_user"
		errDesc := "The resource owner rejected the request"
		rejectReq, _, err := global.Hydra.Admin.OAuth2Api.RejectOAuth2ConsentRequest(context.Background()).ConsentChallenge(oauth2challenge).RejectOAuth2Request(client.RejectOAuth2Request{
			Error:            &errId,
			ErrorDescription: &errDesc,
		}).Execute()
		if err != nil {
			global.Logger.Errorf("Failed to reject consent request with error: %v", err)
			ctx.HTML(http.StatusInternalServerError, "error.tmpl", gin.H{
				"error": "Failed to reject consent request",
			})
			return
		}

		ctx.Redirect(http.StatusTemporaryRedirect, rejectReq.RedirectTo)

		global.Logger.Debugf("User should now be redirecting to target URI.")
	} else if req.Action == "accept" {
		global.Logger.Debugf("User accepted the request, reporting back to hydra...")
		// Retrieve context
		global.Logger.Debugf("Retrieving context...")
		var acceptCtx types.SessionContext
		sessKey = fmt.Sprintf(consts.REDIS_KEY_SHARE_CONTEXT, *consentReq.Subject)
		acceptCtxBytes, err := global.Redis.Get(context.Background(), sessKey).Bytes()
		if err != nil {
			global.Logger.Errorf("Failed to retrieve context with error: %v", err)
			ctx.HTML(http.StatusInternalServerError, "error.tmpl", gin.H{
				"error": "Failed to retrieve context",
			})
			return
		}

		global.Logger.Debugf("Decoding context...")
		err = json.Unmarshal(acceptCtxBytes, &acceptCtx)
		if err != nil {
			global.Logger.Errorf("Failed to parse context with error: %v", err)
			ctx.HTML(http.StatusInternalServerError, "error.tmpl", gin.H{
				"error": "Failed to parse context",
			})
			return
		}

		global.Logger.Debugf("Initializing ID Token...")
		rememberFor := int64(consts.TIME_CONSENT_SESSION_VALID / time.Second)
		acceptReq, _, err := global.Hydra.Admin.OAuth2Api.AcceptOAuth2ConsentRequest(context.Background()).ConsentChallenge(oauth2challenge).AcceptOAuth2ConsentRequest(client.AcceptOAuth2ConsentRequest{
			GrantScope:               consentReq.RequestedScope, // TODO: Specify scopes
			GrantAccessTokenAudience: consentReq.RequestedAccessTokenAudience,
			Remember:                 &req.Remember,
			RememberFor:              &rememberFor,
		}).Execute()
		if err != nil {
			global.Logger.Errorf("Failed to accept consent request with error: %v", err)
			ctx.HTML(http.StatusInternalServerError, "error.tmpl", gin.H{
				"error": "Failed to accept consent request",
			})
			return
		}

		ctx.Redirect(http.StatusTemporaryRedirect, acceptReq.RedirectTo)

		global.Logger.Debugf("User should now be redirecting to target URI.")

	} else {
		global.Logger.Errorf("Undefined consent action: %s", req.Action)
		ctx.HTML(http.StatusBadRequest, "error.tmpl", gin.H{
			"error": "Undefined consent action",
		})
		return
	}

}
