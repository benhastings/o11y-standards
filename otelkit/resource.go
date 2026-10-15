package otelkit

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
)

// NewResource builds the service resource. Precedence, lowest to highest:
// Config fields, convenience env vars (SERVICE_VERSION, SERVICE_NAMESPACE,
// SERVICE_TEAM, DEPLOYMENT_ENVIRONMENT, GIT_COMMIT_SHA, POD_UID), then
// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES.
//
// k8s.* and cloud.* are added by the collector (k8sattributes and
// resourcedetection processors), not here.
func NewResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	info, _ := debug.ReadBuildInfo()

	var kvs []attribute.KeyValue
	put := func(k attribute.Key, candidates ...string) {
		for _, v := range candidates {
			if v != "" {
				kvs = append(kvs, k.String(v))
				return
			}
		}
	}
	put(attrs.ServiceName, cfg.ServiceName)
	put(attrs.ServiceNamespace, os.Getenv("SERVICE_NAMESPACE"), cfg.ServiceNamespace)
	put(attrs.ServiceVersion, os.Getenv("SERVICE_VERSION"), cfg.Version, buildVersion(info))
	put(attrs.ServiceInstanceID, os.Getenv("SERVICE_INSTANCE_ID"), os.Getenv("POD_UID"), newUUID())
	put(attrs.DeploymentEnvironmentName, os.Getenv("DEPLOYMENT_ENVIRONMENT"))
	put(attrs.Kfo11yServiceTeam, os.Getenv("SERVICE_TEAM"), cfg.Team)
	// Go embeds the commit in binaries built from a VCS checkout, so the
	// CI-injected GIT_COMMIT_SHA is a fallback for builds without it.
	put(attrs.VCSRefHeadRevision, os.Getenv("GIT_COMMIT_SHA"), buildSetting(info, "vcs.revision"))

	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithContainer(),
		// Deliberately not resource.WithProcess(): it records the full command
		// line and the process owner, which can leak secrets.
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithAttributes(kvs...),
		resource.WithFromEnv(), // last, so OTEL_* env vars win
	)
	// A detector that can't run (no container ID outside Kubernetes, for
	// example) yields a partial but usable resource. Don't fail startup.
	if errors.Is(err, resource.ErrPartialResource) && res != nil {
		err = nil
	}
	return res, err
}

func buildVersion(info *debug.BuildInfo) string {
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	return info.Main.Version
}

func buildSetting(info *debug.BuildInfo, key string) string {
	if info == nil {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// newUUID returns a random version 4 UUID, used for service.instance.id when
// neither SERVICE_INSTANCE_ID nor POD_UID is set.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
