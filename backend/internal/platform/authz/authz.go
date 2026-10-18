// Package authz wraps the OpenFGA Go SDK. It is the only package that talks to OpenFGA;
// use cases call Check before touching data and the worker uses WriteTuple/DeleteTuple to
// keep OpenFGA in sync with identity.membership.
package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	openfga "github.com/openfga/go-sdk"
	"github.com/openfga/go-sdk/client"
	"github.com/openfga/go-sdk/credentials"
)

var (
	// ErrDenied means OpenFGA answered that the user does not have the requested relation.
	ErrDenied = errors.New("authz: denied")
	// ErrUnavailable means the OpenFGA call failed (network, timeout, misconfiguration).
	// Callers should fail closed.
	ErrUnavailable = errors.New("authz: unavailable")
)

// Config is what is needed to connect to one OpenFGA store.
type Config struct {
	// Addr is the host:port of the OpenFGA server (without scheme).
	Addr string
	// StoreID is the OpenFGA store to use. If empty, StoreName is used to find or create one.
	StoreID string
	// StoreName is the name to look for when StoreID is empty. Ignored when StoreID is set.
	StoreName string
	// ModelID is the authorization model to use. If empty, the latest model in the store is used.
	ModelID string
	// PresharedKey is the OpenFGA pre-shared key (Authorization: Bearer). Required outside local.
	PresharedKey string
	// TLS enables HTTPS. Required outside local development.
	TLS bool
	// Timeout bounds every OpenFGA call.
	Timeout time.Duration
}

// Client is a thin, timeout-bound wrapper around the OpenFGA SDK.
type Client struct {
	api     *client.OpenFgaClient
	storeID string
	modelID string
	timeout time.Duration
}

// New creates a Client and, when necessary, discovers/creates the store and the latest model.
func New(ctx context.Context, cfg Config) (*Client, error) {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	cc := client.ClientConfiguration{
		ApiUrl:               scheme + "://" + cfg.Addr,
		StoreId:              cfg.StoreID,
		AuthorizationModelId: cfg.ModelID,
	}
	if cfg.PresharedKey != "" {
		cc.Credentials = &credentials.Credentials{
			Method: credentials.CredentialsMethodApiToken,
			Config: &credentials.Config{ApiToken: cfg.PresharedKey},
		}
	}
	api, err := client.NewSdkClient(&cc)
	if err != nil {
		return nil, fmt.Errorf("new sdk client: %w", err)
	}

	c := &Client{api: api, timeout: cfg.Timeout}

	storeID := cfg.StoreID
	if storeID == "" {
		name := cfg.StoreName
		if name == "" {
			name = "pitbox"
		}
		id, err := c.ensureStore(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("ensure store: %w", err)
		}
		storeID = id
	}
	if err := c.api.SetStoreId(storeID); err != nil {
		return nil, fmt.Errorf("set store id: %w", err)
	}
	c.storeID = storeID

	modelID := cfg.ModelID
	if modelID == "" {
		id, err := c.readLatestModel(ctx)
		if err != nil {
			return nil, fmt.Errorf("read latest model: %w", err)
		}
		modelID = id
	}
	if err := c.api.SetAuthorizationModelId(modelID); err != nil {
		return nil, fmt.Errorf("set authorization model id: %w", err)
	}
	c.modelID = modelID

	return c, nil
}

// StoreID returns the OpenFGA store ID in use.
func (c *Client) StoreID() string { return c.storeID }

// ModelID returns the authorization model ID in use.
func (c *Client) ModelID() string { return c.modelID }

// Check asks OpenFGA whether the user has the relation on the object.
func (c *Client) Check(ctx context.Context, user string, relation, object string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.api.Check(ctx).Body(client.ClientCheckRequest{
		User:     user,
		Relation: relation,
		Object:   object,
	}).Execute()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if resp.GetAllowed() {
		return nil
	}
	return ErrDenied
}

// ListObjects returns the objects of a given type that the user has the relation with.
func (c *Client) ListObjects(ctx context.Context, user string, relation, objectType string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.api.ListObjects(ctx).Body(client.ClientListObjectsRequest{
		User:     user,
		Relation: relation,
		Type:     objectType,
	}).Execute()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return resp.GetObjects(), nil
}

// WriteTuple writes a relationship tuple. It is used by the worker that syncs membership.
func (c *Client) WriteTuple(ctx context.Context, user, relation, object string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	_, err := c.api.WriteTuples(ctx).Body([]client.ClientTupleKey{
		{User: user, Relation: relation, Object: object},
	}).Execute()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// DeleteTuple deletes a relationship tuple.
func (c *Client) DeleteTuple(ctx context.Context, user, relation, object string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	_, err := c.api.DeleteTuples(ctx).Body([]client.ClientTupleKeyWithoutCondition{
		{User: user, Relation: relation, Object: object},
	}).Execute()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

func (c *Client) ensureStore(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.api.ListStores(ctx).Execute()
	if err != nil {
		return "", fmt.Errorf("list stores: %w", err)
	}
	for _, s := range resp.GetStores() {
		if s.GetName() == name {
			return s.GetId(), nil
		}
	}

	create, err := c.api.CreateStore(ctx).Body(client.ClientCreateStoreRequest{Name: name}).Execute()
	if err != nil {
		return "", fmt.Errorf("create store: %w", err)
	}
	return create.GetId(), nil
}

func (c *Client) readLatestModel(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.api.ReadLatestAuthorizationModel(ctx).Execute()
	if err != nil {
		return "", fmt.Errorf("read latest authorization model: %w", err)
	}
	model := resp.GetAuthorizationModel()
	if model.Id == "" {
		return "", errors.New("no authorization model found in store")
	}
	return model.Id, nil
}

var _ = openfga.CheckResponse{} // ensure the SDK is imported for the generated types used above.
