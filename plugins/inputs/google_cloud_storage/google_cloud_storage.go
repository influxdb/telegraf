//go:generate ../../../tools/readme_config_includer/generator
package gcs

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

const (
	emulatorHostEnv  = "STORAGE_EMULATOR_HOST"
	defaultOffSetKey = "offset-key.json"
)

//go:embed sample.conf
var sampleConfig string

type GCS struct {
	CredentialsFile     string          `toml:"credentials_file"`
	Bucket              string          `toml:"bucket"`
	Prefix              string          `toml:"key_prefix"`
	OffsetKey           string          `toml:"offset_key"`
	ObjectsPerIteration int             `toml:"objects_per_iteration"`
	Log                 telegraf.Logger `toml:"-"`

	offSet offSet
	parser telegraf.Parser
	client *storage.Client
	ctx    context.Context
}

type offSet struct {
	OffSet string `json:"offSet"`
}

func (gcs *GCS) Init() error {
	gcs.ctx = context.Background()
	err := gcs.setUpClient()
	if err != nil {
		gcs.Log.Error("Could not create client", err)
		return err
	}

	return gcs.setOffset()
}

func (*GCS) SampleConfig() string {
	return sampleConfig
}

func (gcs *GCS) SetParser(parser telegraf.Parser) {
	gcs.parser = parser
}

func (gcs *GCS) Gather(acc telegraf.Accumulator) error {
	query := gcs.createQuery()

	bucketName := gcs.Bucket
	bucket := gcs.client.Bucket(bucketName)
	it := bucket.Objects(gcs.ctx, &query)

	processed := 0

	var name string
	for {
		attrs, err := it.Next()

		if errors.Is(err, iterator.Done) {
			gcs.Log.Infof("Iterated all the keys")
			break
		}

		if err != nil {
			gcs.Log.Errorf("Error during iteration of keys: %v", err)
			return err
		}

		name = attrs.Name

		if !gcs.shouldIgnore(name) {
			if err := gcs.processMeasurementsInObject(name, bucket, acc); err != nil {
				gcs.Log.Errorf("Could not process object %q in bucket %q: %v", name, bucketName, err)
				acc.AddError(fmt.Errorf("could not process object %q in bucket %q: %w", name, bucketName, err))
			}
		}

		processed++

		if gcs.reachedThreshlod(processed) {
			return gcs.updateOffset(bucket, name)
		}
	}

	return gcs.updateOffset(bucket, name)
}

func (gcs *GCS) createQuery() storage.Query {
	if gcs.offSet.isPresent() {
		return storage.Query{Prefix: gcs.Prefix, StartOffset: gcs.offSet.OffSet}
	}

	return storage.Query{Prefix: gcs.Prefix}
}

func (gcs *GCS) shouldIgnore(name string) bool {
	return gcs.offSet.OffSet == name || gcs.OffsetKey == name
}

func (gcs *GCS) processMeasurementsInObject(name string, bucket *storage.BucketHandle, acc telegraf.Accumulator) error {
	gcs.Log.Debugf("Fetching key: %s", name)
	r, err := bucket.Object(name).NewReader(gcs.ctx)
	defer gcs.closeReader(r)

	if err != nil {
		return err
	}

	metrics, err := gcs.fetchedMetrics(r)

	if err != nil {
		return err
	}

	for _, metric := range metrics {
		acc.AddFields(metric.Name(), metric.Fields(), metric.Tags(), metric.Time())
	}

	return nil
}

func (gcs *GCS) fetchedMetrics(r *storage.Reader) ([]telegraf.Metric, error) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, err
	}

	return gcs.parser.Parse(buf.Bytes())
}

func (gcs *GCS) reachedThreshlod(processed int) bool {
	return gcs.ObjectsPerIteration != 0 && processed >= gcs.ObjectsPerIteration
}

func (gcs *GCS) updateOffset(bucket *storage.BucketHandle, name string) error {
	if gcs.shouldIgnore(name) {
		return nil
	}

	offsetModel := newOffset(name)
	marshalled, err := json.Marshal(offsetModel)

	if err != nil {
		return err
	}

	offsetKey := bucket.Object(gcs.OffsetKey)
	writer := offsetKey.NewWriter(gcs.ctx)
	writer.ContentType = "application/json"
	defer writer.Close()

	if _, err := writer.Write(marshalled); err != nil {
		return err
	}

	gcs.offSet = *offsetModel

	return nil
}

func (gcs *GCS) setUpClient() error {
	if endpoint, present := os.LookupEnv(emulatorHostEnv); present {
		return gcs.setUpLocalClient(endpoint)
	}

	return gcs.setUpDefaultClient()
}

func (gcs *GCS) setUpLocalClient(endpoint string) error {
	noAuth := option.WithoutAuthentication()
	endpoints := option.WithEndpoint("http://" + endpoint)
	client, err := storage.NewClient(gcs.ctx, noAuth, endpoints)

	if err != nil {
		return err
	}

	gcs.client = client
	return nil
}

func (gcs *GCS) setUpDefaultClient() error {
	var credentialsOption option.ClientOption

	if gcs.CredentialsFile != "" {
		credentialsOption = option.WithCredentialsFile(gcs.CredentialsFile)
	} else {
		creds, err := google.FindDefaultCredentials(gcs.ctx, storage.ScopeReadOnly)
		if err != nil {
			return fmt.Errorf(
				"unable to find GCP Application Default Credentials: %v."+
					"Either set ADC or provide CredentialsFile config", err)
		}
		credentialsOption = option.WithCredentials(creds)
	}

	client, err := storage.NewClient(gcs.ctx, credentialsOption)
	gcs.client = client
	return err
}

func (gcs *GCS) setOffset() error {
	if gcs.client == nil {
		return errors.New("cannot set offset if client is not set")
	}

	if gcs.OffsetKey != "" {
		gcs.OffsetKey = gcs.Prefix + gcs.OffsetKey
	} else {
		gcs.OffsetKey = gcs.Prefix + defaultOffSetKey
	}

	btk := gcs.client.Bucket(gcs.Bucket)
	obj := btk.Object(gcs.OffsetKey)

	var offSet offSet

	if r, err := obj.NewReader(gcs.ctx); err == nil {
		defer gcs.closeReader(r)
		buf := new(bytes.Buffer)

		if _, err := io.Copy(buf, r); err == nil {
			if marshalError := json.Unmarshal(buf.Bytes(), &offSet); marshalError != nil {
				return marshalError
			}
		}
	} else {
		offSet = *newEmptyOffset()
	}

	gcs.offSet = offSet

	return nil
}

func (gcs *GCS) closeReader(r *storage.Reader) {
	if err := r.Close(); err != nil {
		gcs.Log.Errorf("Could not close reader: %v", err)
	}
}

func newEmptyOffset() *offSet {
	return &offSet{OffSet: ""}
}

func newOffset(offset string) *offSet {
	return &offSet{OffSet: offset}
}

func (offSet *offSet) isPresent() bool {
	return offSet.OffSet != ""
}

func init() {
	inputs.Add("google_cloud_storage", func() telegraf.Input {
		gcs := &GCS{}
		return gcs
	})
}
