package vsock2http2

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"

	"github.com/aybabtme/vsock2http2/example/grpcping"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	certFile = "cert.pem"
	keyFile  = "key.pem"
)

func TestPingWithProxy(t *testing.T) {
	ctx, done := context.WithTimeout(context.Background(), time.Second)
	defer done()

	serverCalls := uint64(0)
	pingSrv := func(ctx context.Context, req *grpcping.PingReq) (*grpcping.PingRes, error) {
		_, ok := metadata.FromIncomingContext(ctx)
		assert.True(t, ok)
		atomic.AddUint64(&serverCalls, 1)
		return &grpcping.PingRes{}, nil
	}

	addr, doneServer := startServer(t, pingSrv)
	defer doneServer()

	proxyl, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer proxyl.Close()

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	http2.ConfigureTransport(tr)

	proxy := &httputil.ReverseProxy{
		ErrorLog:  log.New(os.Stderr, "  [proxy]  ", log.LstdFlags),
		Transport: tr,
		Director: func(r *http.Request) {
			r.Host = addr.Host
			r.URL.Scheme = addr.Scheme
			r.URL.Host = addr.Host
			log.Printf("I GOT YOU==== %s - %s - %s", r.Method, r.URL.String(), r.RemoteAddr)
		},
	}

	go (&http.Server{Handler: proxy}).ServeTLS(proxyl, certFile, keyFile)

	tlc := &tls.Config{
		InsecureSkipVerify: true,
	}

	// cc, err := grpc.Dial(addr.Host, grpc.WithInsecure())
	cc, err := grpc.Dial(proxyl.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(tlc)))
	assert.NoError(t, err)
	defer cc.Close()

	pinger := grpcping.NewPingerClient(cc)

	log.Print("starting")
	res, err := pinger.Ping(ctx, &grpcping.PingReq{})
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.NotZero(t, atomic.LoadUint64(&serverCalls))
}

func startServer(t *testing.T, pinger func(ctx context.Context, req *grpcping.PingReq) (*grpcping.PingRes, error)) (addr *url.URL, done func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	u, err := url.Parse("https://" + l.Addr().String())
	assert.NoError(t, err)

	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	assert.NoError(t, err)

	srv := grpc.NewServer(grpc.Creds(creds))
	grpcping.RegisterPingerServer(srv, &Server{
		PingFunc: pinger,
	})
	go srv.Serve(l)

	return u, func() {
		srv.Stop()
		l.Close()
	}
}

var _ grpcping.PingerServer = (*Server)(nil)

type Server struct {
	PingFunc func(ctx context.Context, req *grpcping.PingReq) (*grpcping.PingRes, error)
}

func (srv *Server) Ping(ctx context.Context, req *grpcping.PingReq) (*grpcping.PingRes, error) {
	if srv.PingFunc != nil {
		return srv.PingFunc(ctx, req)
	}
	return &grpcping.PingRes{}, status.Errorf(codes.Unimplemented, "ping")
}
