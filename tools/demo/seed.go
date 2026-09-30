package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	// seedNamespace holds the seeded objects.
	seedNamespace = "default"
	// seedProgressEvery is the number of created objects between progress lines.
	seedProgressEvery = 100
)

// createBackoff retries creates for up to ~4 minutes, long enough for a shard restart.
var createBackoff = wait.Backoff{
	Duration: time.Second,
	Factor:   2,
	Steps:    8,
	Cap:      time.Minute,
}

// seedOptions configures a seed run.
type seedOptions struct {
	configmaps int
	secrets    int
	// size is the payload bytes per object, objects are limited to 1 MiB.
	size    int
	workers int
}

func (o *seedOptions) register(fs *flag.FlagSet) {
	fs.IntVar(&o.configmaps, "configmaps", 850, "number of configmaps")
	fs.IntVar(&o.secrets, "secrets", 150, "number of secrets")
	fs.IntVar(&o.size, "size", 450*1000, "payload bytes per object, objects are limited to 1 MiB")
	fs.IntVar(&o.workers, "workers", 8, "concurrent creates")
}

func (o *seedOptions) validate() error {
	if o.workers < 1 {
		return errors.New("-workers must be at least 1")
	}
	return nil
}

func seed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	var ws workspaceFlags
	ws.register(fs)
	var opts seedOptions
	opts.register(fs)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}
	if err := ws.validate(); err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}

	_, server, err := ws.servers()
	if err != nil {
		return err
	}
	return seedWorkspace(ctx, ws.kubeconfig, server, opts)
}

// seedWorkspace creates the configmaps and secrets of opts in the workspace at server.
func seedWorkspace(ctx context.Context, kubeconfig, server string, opts seedOptions) error {
	cfg, err := restConfig(kubeconfig, server)
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	payload, err := randomPayload(opts.size)
	if err != nil {
		return err
	}

	s := seeder{
		client:  client,
		payload: payload,
		text:    payloadText(payload),
		start:   time.Now(),
		total:   int64(opts.configmaps + opts.secrets),
	}

	objects := make(chan func(context.Context) error)
	errs := make(chan error, opts.workers)
	var wg sync.WaitGroup
	for range opts.workers {
		wg.Go(func() {
			for create := range objects {
				if err := create(ctx); err != nil {
					errs <- err
					return
				}
			}
		})
	}

	sendErr := s.enqueue(ctx, objects, opts.configmaps, opts.secrets, errs)
	close(objects)
	wg.Wait()
	close(errs)

	if sendErr != nil {
		return sendErr
	}
	if err, ok := <-errs; ok {
		return err
	}
	fmt.Printf("created %d objects in %s\n", s.created.Load(), time.Since(s.start).Round(time.Second))
	return nil
}

// seeder creates the objects of one seed run.
type seeder struct {
	client  kubernetes.Interface
	payload []byte
	// text is payload as base64, cut to the same length, for configmap data.
	text    string
	start   time.Time
	total   int64
	created atomic.Int64
}

// enqueue feeds one create per object into objects until done, ctx ends or a worker failed.
func (s *seeder) enqueue(ctx context.Context, objects chan<- func(context.Context) error, configmaps, secrets int, errs <-chan error) error {
	var creates []func(context.Context) error
	for i := range configmaps {
		creates = append(creates, s.configMap(fmt.Sprintf("bulk-%05d", i)))
	}
	for i := range secrets {
		creates = append(creates, s.secret(fmt.Sprintf("bulk-%05d", i)))
	}

	for _, create := range creates {
		select {
		case <-ctx.Done():
			return fmt.Errorf("seeding: %w", ctx.Err())
		case err := <-errs:
			return err
		case objects <- create:
		}
	}
	return nil
}

func (s *seeder) configMap(name string) func(context.Context) error {
	return func(ctx context.Context) error {
		cm := seedConfigMap(name, s.text)
		return s.create(ctx, "configmap "+name, func(ctx context.Context) error {
			_, err := s.client.CoreV1().ConfigMaps(seedNamespace).Create(ctx, cm, metav1.CreateOptions{})
			return err //nolint:wrapcheck // wrapped by create
		})
	}
}

func (s *seeder) secret(name string) func(context.Context) error {
	return func(ctx context.Context) error {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: map[string][]byte{
				"payload": s.payload,
			},
		}
		return s.create(ctx, "secret "+name, func(ctx context.Context) error {
			_, err := s.client.CoreV1().Secrets(seedNamespace).Create(ctx, secret, metav1.CreateOptions{})
			return err //nolint:wrapcheck // wrapped by create
		})
	}
}

// create runs fn with retries, an existing object counts as created.
func (s *seeder) create(ctx context.Context, what string, fn func(context.Context) error) error {
	err := retry.OnError(createBackoff, retriable, func() error {
		err := fn(ctx)
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("creating %s: %w", what, err)
	}

	if n := s.created.Add(1); n%seedProgressEvery == 0 || n == s.total {
		fmt.Printf("%d/%d objects after %s\n", n, s.total, time.Since(s.start).Round(time.Second))
	}
	return nil
}

// retriable reports whether a create may succeed on retry.
func retriable(err error) bool {
	switch {
	case apierrors.IsInvalid(err),
		apierrors.IsForbidden(err),
		apierrors.IsUnauthorized(err),
		apierrors.IsRequestEntityTooLargeError(err),
		errors.Is(err, context.Canceled):
		return false
	default:
		return true
	}
}

// randomPayload returns size random bytes.
func randomPayload(size int) ([]byte, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generating payload: %w", err)
	}
	return b, nil
}

// payloadText returns payload as base64, cut to the payload length, for configmap data.
func payloadText(payload []byte) string {
	return base64.StdEncoding.EncodeToString(payload)[:len(payload)]
}

// seedConfigMap returns a seeded configmap.
func seedConfigMap(name, payload string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: seedNamespace,
		},
		Data: map[string]string{
			"payload": payload,
		},
	}
}
