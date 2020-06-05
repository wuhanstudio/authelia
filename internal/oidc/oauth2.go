package oidc

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/authelia/authelia/internal/configuration/schema"
	"github.com/authelia/authelia/internal/middlewares"
	"github.com/authelia/authelia/internal/utils"
	"github.com/fasthttp/router"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/openid"
	"github.com/ory/fosite/storage"
	"github.com/ory/fosite/token/jwt"
)

func NewStore() *storage.MemoryStore {
	return &storage.MemoryStore{
		IDSessions: make(map[string]fosite.Requester),
		Clients: map[string]fosite.Client{
			"oidc-tester-app": &fosite.DefaultClient{
				ID:            "oidc-tester-app",
				Secret:        []byte(`$2a$10$IxMdI6d.LIRZPpSfEwNoeu4rY3FhDREsxFJXikcgdRRAStxUlsuEO`), // = "foobar"
				RedirectURIs:  []string{"https://oidc.example.com:8080/oauth2/callback"},
				ResponseTypes: []string{"code"},
				GrantTypes:    []string{"implicit", "refresh_token", "authorization_code"},
				Scopes:        []string{"openid"},
			},
		},
		Users: map[string]storage.MemoryUserRelation{
			"john": {
				// This store simply checks for equality, a real storage implementation would obviously use
				// a hashing algorithm for encrypting the user password.
				Username: "john",
				Password: "secret",
			},
		},
		AuthorizeCodes:         map[string]storage.StoreAuthorizeCode{},
		AccessTokens:           map[string]fosite.Requester{},
		RefreshTokens:          map[string]fosite.Requester{},
		PKCES:                  map[string]fosite.Requester{},
		AccessTokenRequestIDs:  map[string]string{},
		RefreshTokenRequestIDs: map[string]string{},
	}
}

func InitializeOIDC(configuration *schema.OpenIDConnectConfiguration, router *router.Router, autheliaMiddleware middlewares.RequestHandlerBridge) {
	// This is an exemplary storage instance. We will add a client and a user to it so we can use these later on.
	var store = NewStore()

	var oidcConfig = new(compose.Config)

	b, err := ioutil.ReadFile(configuration.OIDCIssuerPrivateKeyPath)
	if err != nil {
		panic(err)
	}

	privateKey, err := utils.ParseRsaPrivateKeyFromPemStr(string(b))
	if err != nil {
		panic(err)
	}

	// Because we are using oauth2 and open connect id, we use this little helper to combine the two in one
	// variable.
	var start = compose.CommonStrategy{
		CoreStrategy:               compose.NewOAuth2HMACStrategy(oidcConfig, []byte(configuration.OAuth2HMACSecret), nil),
		OpenIDConnectTokenStrategy: compose.NewOpenIDConnectStrategy(oidcConfig, privateKey),
	}

	var oauth2 = compose.Compose(
		oidcConfig,
		store,
		start,
		nil,

		// enabled handlers
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2AuthorizeImplicitFactory,
		compose.OAuth2ClientCredentialsGrantFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2ResourceOwnerPasswordCredentialsFactory,

		compose.OAuth2TokenRevocationFactory,
		compose.OAuth2TokenIntrospectionFactory,

		// be aware that open id connect factories need to be added after oauth2 factories to work properly.
		compose.OpenIDConnectExplicitFactory,
		compose.OpenIDConnectImplicitFactory,
		compose.OpenIDConnectHybridFactory,
		compose.OpenIDConnectRefreshFactory,
	)

	// OpenID Connect discovery: https://openid.net/specs/openid-connect-discovery-1_0.html#ProviderConfigurationRequest
	router.GET("/.well-known/openid-configuration", autheliaMiddleware(WellKnownConfigurationGet))

	router.GET("/api/oidc/jwks", autheliaMiddleware(JWKsGet(&privateKey.PublicKey)))
	router.GET("/api/oidc/auth", autheliaMiddleware(middlewares.NewHTTPToAutheliaHandlerAdaptor(AuthEndpointGet(oauth2))))
	router.POST("/api/oidc/token", autheliaMiddleware(middlewares.NewHTTPToAutheliaHandlerAdaptor(tokenEndpoint(oauth2))))

	// revoke tokens
	// http.HandleFunc("/oauth2/revoke", revokeEndpoint)
	// http.HandleFunc("/oauth2/introspect", introspectionEndpoint)
}

// A session is passed from the `/auth` to the `/token` endpoint. You probably want to store data like: "Who made the request",
// "What organization does that person belong to" and so on.
// For our use case, the session will meet the requirements imposed by JWT access tokens, HMAC access tokens and OpenID Connect
// ID Tokens plus a custom field

// newSession is a helper function for creating a new session. This may look like a lot of code but since we are
// setting up multiple strategies it is a bit longer.
// Usually, you could do:
//
//  session = new(fosite.DefaultSession)
func newSession(user string) *openid.DefaultSession {
	extra := map[string]interface{}{
		"email": fmt.Sprintf("%s@authelia.com", user),
	}

	return &openid.DefaultSession{
		Claims: &jwt.IDTokenClaims{
			Issuer:      "https://login.example.com:8080",
			Subject:     user,
			Audience:    []string{"https://oidc.example.com:8080"},
			ExpiresAt:   time.Now().Add(time.Hour * 6),
			IssuedAt:    time.Now(),
			RequestedAt: time.Now(),
			AuthTime:    time.Now(),
			Extra:       extra,
		},
		Headers: &jwt.Headers{
			Extra: make(map[string]interface{}),
		},
	}
}
