package transport

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	setupv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/setup/v1/setupv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

const testID = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60"

func TestSetupContract(t *testing.T) {
	v, err := protovalidate.New()
	require.NoError(t, err)

	validSetup := func() *setupv1.CreateSetupRequest {
		return &setupv1.CreateSetupRequest{
			TeamId: testID, RequestId: testID, CarId: testID, Name: "Base",
		}
	}
	require.NoError(t, v.Validate(validSetup()))
	req := validSetup()
	req.Name = strings.Repeat("x", 121)
	require.Error(t, v.Validate(req))
	req = validSetup()
	req.CarId = "x"
	require.Error(t, v.Validate(req))

	version := &setupv1.CreateVersionRequest{
		TeamId: testID, RequestId: testID, SetupId: testID, BaseVersion: 1,
	}
	require.NoError(t, v.Validate(version))
	version.BaseVersion = 0
	require.Error(t, v.Validate(version))

	get := &setupv1.GetVersionRequest{TeamId: testID, SetupId: testID}
	require.NoError(t, v.Validate(get))
	get.Version = proto.Int32(0)
	require.Error(t, v.Validate(get))

	link := &setupv1.CreateShareLinkRequest{
		TeamId: testID, RequestId: testID, SetupId: testID,
		ExpiresIn: setupv1.ShareLinkExpiry_SHARE_LINK_EXPIRY_24H,
	}
	require.NoError(t, v.Validate(link))
	link.ExpiresIn = setupv1.ShareLinkExpiry_SHARE_LINK_EXPIRY_UNSPECIFIED
	require.Error(t, v.Validate(link))

	shared := &setupv1.GetSharedSetupRequest{Token: "abc"}
	require.NoError(t, v.Validate(shared))
	shared.Token = ""
	require.Error(t, v.Validate(shared))
	shared.Token = strings.Repeat("x", 129)
	require.Error(t, v.Validate(shared))
}

func TestStructConversionRejectsNonFiniteAndKeepsDepth(t *testing.T) {
	good := map[string]any{"diffs": map[string]any{"front_cst": 500.0}}
	s, err := structpb.NewStruct(good)
	require.NoError(t, err)
	got, err := structToMap(s)
	require.NoError(t, err)
	require.Equal(t, good, got)

	bad := &structpb.Value{Kind: &structpb.Value_NumberValue{NumberValue: math.NaN()}}
	req := &structpb.Struct{Fields: map[string]*structpb.Value{"x": bad}}
	_, err = structToMap(req)
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve)
	require.Equal(t, domain.RuleType, ve.RuleID)

	// A number nested too deeply is refused during the descent.
	inner := &structpb.Struct{Fields: map[string]*structpb.Value{"x": structpb.NewStringValue("leaf")}}
	for i := 0; i <= domain.MaxDataDepth; i++ {
		inner = &structpb.Struct{Fields: map[string]*structpb.Value{"x": structpb.NewStructValue(inner)}}
	}
	_, err = structToMap(inner)
	require.Error(t, err)
}

func TestSetupErrorCarriesOnlyRuleAndPath(t *testing.T) {
	err := setupError(&domain.ValidationError{Path: "diffs.front_cst", RuleID: domain.RuleRange})
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	require.Equal(t, connect.CodeInvalidArgument, ce.Code())
	require.Equal(t, msgInvalidSetupData, ce.Message())
	require.Len(t, ce.Details(), 1)
	require.Equal(t, "pitbox.setup.v1.SetupDataViolation", ce.Details()[0].Type)
	require.NotContains(t, string(ce.Details()[0].Value), "987654321")
}

func FuzzDataFromProto(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte(`{"diffs":{"front_cst":500}}`))
	f.Add([]byte(`{"bars":{"front_removed":true}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var s structpb.Struct
		if err := proto.Unmarshal(raw, &s); err != nil {
			return
		}
		_, _ = structToMap(&s)
	})
}

func TestSharedTransportNoStore(t *testing.T) {
	fake := &fakeShared{}
	srv := connect.NewServer()
	setupv1connect.RegisterSharedSetupServiceHandler(srv, &Shared{svc: fake})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, srv)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := setupv1connect.NewSharedSetupServiceClient(
		connect.NewClient(connecthttp.NewTransport(ts.Client(), ts.URL)),
	)
	ctx, info := connect.NewClientContext(context.Background())
	_, err := client.GetSharedSetup(ctx, &setupv1.GetSharedSetupRequest{Token: "abc"})
	require.NoError(t, err)
	require.Equal(t, "no-store", info.ResponseHeader().Get("Cache-Control"))
	require.Equal(t, 1, fake.calls)
}

type fakeShared struct{ calls int }

func (f *fakeShared) OpenShareLink(context.Context, string) (domain.SharedSetup, error) {
	f.calls++
	return domain.SharedSetup{Name: "x", Version: 1, SchemaVersion: 1, Data: map[string]any{}}, nil
}
