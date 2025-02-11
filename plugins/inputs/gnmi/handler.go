package gnmi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/proto/gnmi_ext"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/common/yangmodel"
	"github.com/influxdata/telegraf/plugins/inputs/gnmi/extensions/jnpr_gnmi_extention"
	"github.com/influxdata/telegraf/selfstat"
)

const eidJuniperTelemetryHeader = 1

type handler struct {
	host                          string
	port                          string
	aliases                       map[*pathInfo]string
	tagsubs                       []tagSubscription
	maxMsgSize                    int
	emptyNameWarnShown            bool
	vendorExt                     []string
	tagStore                      *tagStore
	trace                         bool
	canonicalFieldNames           bool
	trimSlash                     bool
	tagPathPrefix                 bool
	guessPathStrategy             string
	decoder                       *yangmodel.Decoder
	enforceFirstNamespaceAsOrigin bool
	log                           telegraf.Logger
	keepalive.ClientParameters
}

// SubscribeGNMI and extract telemetry data
func (h *handler) subscribeGNMI(ctx context.Context, acc telegraf.Accumulator, tlscfg *tls.Config, request *gnmi.SubscribeRequest) error {
	var creds credentials.TransportCredentials
	if tlscfg != nil {
		creds = credentials.NewTLS(tlscfg)
	} else {
		creds = insecure.NewCredentials()
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}

	if h.maxMsgSize > 0 {
		opts = append(opts, grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(h.maxMsgSize),
		))
	}

	if h.ClientParameters.Time > 0 {
		opts = append(opts, grpc.WithKeepaliveParams(h.ClientParameters))
	}

	// Used to report the status of the TCP connection to the device. If the
	// GNMI connection goes down, but TCP is still up this will still report
	// connected until the TCP connection times out.
	connectStat := selfstat.Register("gnmi", "grpc_connection_status", map[string]string{"source": h.host})
	defer connectStat.Set(0)

	address := net.JoinHostPort(h.host, h.port)
	client, err := grpc.NewClient(address, opts...)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer client.Close()

	subscribeClient, err := gnmi.NewGNMIClient(client).Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup subscription: %w", err)
	}

	// If io.EOF is returned, the stream may have ended and stream status
	// can be determined by calling Recv.
	if err := subscribeClient.Send(request); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to send subscription request: %w", err)
	}
	connectStat.Set(1)
	h.log.Debugf("Connection to gNMI device %s established", address)

	defer h.log.Debugf("Connection to gNMI device %s closed", address)
	for ctx.Err() == nil {
		var reply *gnmi.SubscribeResponse
		if reply, err = subscribeClient.Recv(); err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				return fmt.Errorf("aborted gNMI subscription: %w", err)
			}
			break
		}

		if h.trace {
			buf, err := protojson.Marshal(reply)
			if err != nil {
				h.log.Debugf("Marshal failed: %v", err)
			} else {
				t := reply.GetUpdate().GetTimestamp()
				h.log.Debugf("Got update_%v: %s", t, string(buf))
			}
		}
		if response, ok := reply.Response.(*gnmi.SubscribeResponse_Update); ok {
			h.handleSubscribeResponseUpdate(acc, response, reply.GetExtension())
		}
	}
	return nil
}

// Handle SubscribeResponse_Update message from gNMI and parse contained telemetry data
func (h *handler) handleSubscribeResponseUpdate(acc telegraf.Accumulator, response *gnmi.SubscribeResponse_Update, extension []*gnmi_ext.Extension) {
	grouper := metric.NewSeriesGrouper()
	timestamp := time.Unix(0, response.Update.Timestamp)

	// Extract tags from potential extension in the update notification
	headerTags := make(map[string]string)
	for _, ext := range extension {
		currentExt := ext.GetRegisteredExt().Msg
		if currentExt == nil {
			break
		}

		switch ext.GetRegisteredExt().Id {
		case eidJuniperTelemetryHeader:
			// Juniper Header extension
			// Decode it only if user requested it
			if choice.Contains("juniper_header", h.vendorExt) {
				juniperHeader := &jnpr_gnmi_extention.GnmiJuniperTelemetryHeaderExtension{}
				if err := proto.Unmarshal(currentExt, juniperHeader); err != nil {
					h.log.Errorf("unmarshal gnmi Juniper Header extension failed: %v", err)
				} else {
					// Add only relevant Tags from the Juniper Header extension.
					// These are required for aggregation
					headerTags["component_id"] = strconv.FormatUint(uint64(juniperHeader.GetComponentId()), 10)
					headerTags["component"] = juniperHeader.GetComponent()
					headerTags["sub_component_id"] = strconv.FormatUint(uint64(juniperHeader.GetSubComponentId()), 10)
				}
			}
		default:
			continue
		}
	}

	// Extract the path part valid for the whole set of updates if any
	prefix := newInfoFromPath(response.Update.Prefix)
	if h.enforceFirstNamespaceAsOrigin {
		prefix.enforceFirstNamespaceAsOrigin()
	}

	// Add info to the tags
	headerTags["source"] = h.host
	if !prefix.empty() {
		headerTags["path"] = prefix.fullPath()
	}

	// Process and remove tag-updates from the response first so we can
	// add all available tags to the metrics later.
	var valueFields []updateField
	for _, update := range response.Update.Update {
		fullPath := prefix.append(update.Path)
		if h.enforceFirstNamespaceAsOrigin {
			prefix.enforceFirstNamespaceAsOrigin()
		}
		if update.Path.Origin != "" {
			fullPath.origin = update.Path.Origin
		}

		fields, err := h.newFieldsFromUpdate(fullPath, update)
		if err != nil {
			h.log.Errorf("Processing update %v failed: %v", update, err)
		}

		// Prepare tags from prefix
		tags := make(map[string]string, len(headerTags))
		for key, val := range headerTags {
			tags[key] = val
		}
		for key, val := range fullPath.tags(h.tagPathPrefix) {
			tags[key] = val
		}

		// TODO: Handle each field individually to allow in-JSON tags
		var tagUpdate bool
		for _, tagSub := range h.tagsubs {
			if !fullPath.equalsPathNoKeys(tagSub.fullPath) {
				continue
			}
			h.log.Debugf("Tag-subscription update for %q: %+v", tagSub.Name, update)
			if err := h.tagStore.insert(tagSub, fullPath, fields, tags); err != nil {
				h.log.Errorf("Inserting tag failed: %v", err)
			}
			tagUpdate = true
			break
		}
		if !tagUpdate {
			valueFields = append(valueFields, fields...)
		}
	}

	// Some devices do not provide a prefix, so do some guesswork based
	// on the paths of the fields
	if headerTags["path"] == "" && h.guessPathStrategy == "common path" {
		if prefixPath := guessPrefixFromUpdate(valueFields); prefixPath != "" {
			headerTags["path"] = prefixPath
		}
	}

	// Parse individual update message and create measurements
	for _, field := range valueFields {
		if field.path.empty() {
			continue
		}

		// Prepare tags from prefix
		fieldTags := field.path.tags(h.tagPathPrefix)
		tags := make(map[string]string, len(headerTags)+len(fieldTags))
		for key, val := range headerTags {
			tags[key] = val
		}
		for key, val := range fieldTags {
			tags[key] = val
		}

		// Add the tags derived via tag-subscriptions
		for k, v := range h.tagStore.lookup(field.path, tags) {
			tags[k] = v
		}

		// Lookup alias for the metric
		aliasPath, name := h.lookupAlias(field.path)
		if name == "" {
			h.log.Debugf("No measurement alias for gNMI path: %s", field.path)
			if !h.emptyNameWarnShown {
				if buf, err := json.Marshal(response); err == nil {
					h.log.Warnf(emptyNameWarning, field.path, string(buf))
				} else {
					h.log.Warnf(emptyNameWarning, field.path, response.Update)
				}
				h.emptyNameWarnShown = true
			}
		}

		aliasInfo := newInfoFromString(aliasPath)
		if h.enforceFirstNamespaceAsOrigin {
			aliasInfo.enforceFirstNamespaceAsOrigin()
		}

		if tags["path"] == "" && h.guessPathStrategy == "subscription" {
			tags["path"] = aliasInfo.String()
		}

		// Group metrics
		var key string
		if h.canonicalFieldNames {
			// Strip the origin is any for the field names
			field.path.origin = ""
			key = field.path.String()
			key = strings.ReplaceAll(key, "-", "_")
		} else {
			// If the alias is a subpath of the field path and the alias is
			// shorter than the full path to avoid an empty key, then strip the
			// common part of the field is prefixed with the alias path. Note
			// the origins can match or be empty and be considered equal.
			if relative := aliasInfo.relative(field.path, true); relative != "" {
				key = relative
			} else {
				// Otherwise use the last path element as the field key
				key = field.path.base()
			}
			key = strings.ReplaceAll(key, "-", "_")
		}
		if h.trimSlash {
			key = strings.TrimLeft(key, "/.")
		}
		if key == "" {
			h.log.Errorf("Invalid empty path %q with alias %q", field.path.String(), aliasPath)
			continue
		}
		grouper.Add(name, tags, timestamp, key, field.value)
	}

	// Add grouped measurements
	for _, metricToAdd := range grouper.Metrics() {
		acc.AddMetric(metricToAdd)
	}
}

// Try to find the alias for the given path
type aliasCandidate struct {
	path, alias string
}

func (h *handler) lookupAlias(info *pathInfo) (aliasPath, alias string) {
	candidates := make([]aliasCandidate, 0)
	for i, a := range h.aliases {
		if !i.isSubPathOf(info) {
			continue
		}
		candidates = append(candidates, aliasCandidate{i.String(), a})
	}
	if len(candidates) == 0 {
		return "", ""
	}

	// Reverse sort the candidates by path length so we can use the longest match
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i].path) > len(candidates[j].path)
	})

	return candidates[0].path, candidates[0].alias
}

func guessPrefixFromUpdate(fields []updateField) string {
	if len(fields) == 0 {
		return ""
	}
	if len(fields) == 1 {
		return fields[0].path.dir()
	}
	segments := make([]segment, 0, len(fields[0].path.segments))
	commonPath := &pathInfo{
		origin:   fields[0].path.origin,
		segments: append(segments, fields[0].path.segments...),
	}
	for _, f := range fields[1:] {
		commonPath.keepCommonPart(f.path)
	}
	if commonPath.empty() {
		return ""
	}
	return commonPath.String()
}
