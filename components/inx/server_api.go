package inx

import (
	"bytes"
	"context"
	"net/http/httptest"
	"net/url"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inx "github.com/iotaledger/inx/go"
)

func (s *Server) RegisterAPIRoute(_ context.Context, req *inx.APIRouteRequest) (*inx.NoParams, error) {
	if len(req.GetRoute()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "route can not be empty")
	}
	if len(req.GetHost()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "host can not be empty")
	}
	if req.GetPort() == 0 {
		return nil, status.Error(codes.InvalidArgument, "port can not be zero")
	}
	if err := deps.RestRouteManager.AddProxyRoute(req.GetRoute(), req.GetHost(), req.GetPort(), req.GetPath()); err != nil {
		Component.LogErrorf("Error registering proxy %s", req.GetRoute())

		return nil, status.Errorf(codes.Internal, "error adding route to proxy: %s", err.Error())
	}
	Component.LogInfof("Registered proxy %s => %s:%d", req.GetRoute(), req.GetHost(), req.GetPort())

	return &inx.NoParams{}, nil
}

func (s *Server) UnregisterAPIRoute(_ context.Context, req *inx.APIRouteRequest) (*inx.NoParams, error) {
	if len(req.GetRoute()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "route can not be empty")
	}
	deps.RestRouteManager.RemoveRoute(req.GetRoute())
	Component.LogInfof("Removed proxy %s", req.GetRoute())

	return &inx.NoParams{}, nil
}

func (s *Server) PerformAPIRequest(_ context.Context, req *inx.APIRequest) (*inx.APIResponse, error) {
	httpReq := httptest.NewRequest(req.GetMethod(), req.GetPath(), bytes.NewBuffer(req.GetBody()))
	httpReq.Header = req.HTTPHeader()

	rec := httptest.NewRecorder()
	c := deps.Echo.NewContext(httpReq, rec)

	reqURL, err := url.Parse(req.GetPath())
	if err != nil {
		return nil, err
	}

	deps.Echo.Router().Find(req.GetMethod(), reqURL.Path, c)
	if err := c.Handler()(c); err != nil {
		return nil, err
	}

	return &inx.APIResponse{
		Code:    uint32(rec.Code),
		Headers: inx.HeadersFromHTTPHeader(rec.Header()),
		Body:    rec.Body.Bytes(),
	}, nil
}
