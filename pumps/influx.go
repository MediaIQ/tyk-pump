package pumps

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/influxdata/influxdb/client/v2"
	"github.com/mitchellh/mapstructure"

	"github.com/TykTechnologies/tyk-pump/analytics"
)

type InfluxPump struct {
	dbConf *InfluxConf
	CommonPumpConfig
}

var (
	influxPrefix     = "influx-pump"
	influxDefaultENV = PUMPS_ENV_PREFIX + "_INFLUX" + PUMPS_ENV_META_PREFIX
	table            = "analytics"
)

type InfluxConf struct {
	EnvPrefix    string   `mapstructure:"meta_env_prefix"`
	DatabaseName string   `mapstructure:"database_name"`
	Addr         string   `mapstructure:"address"`
	Username     string   `mapstructure:"username"`
	Password     string   `mapstructure:"password"`
	Fields       []string `mapstructure:"fields"`
	Tags         []string `mapstructure:"tags"`
}

func (i *InfluxPump) New() Pump {
	newPump := InfluxPump{}
	return &newPump
}

func (i *InfluxPump) GetName() string {
	return "InfluxDB Pump"
}

func (i *InfluxPump) GetEnvPrefix() string {
	return i.dbConf.EnvPrefix
}

func (i *InfluxPump) Init(config interface{}) error {
	i.dbConf = &InfluxConf{}
	i.log = log.WithField("prefix", influxPrefix)

	err := mapstructure.Decode(config, &i.dbConf)
	if err != nil {
		i.log.Fatal("Failed to decode configuration: ", err)
	}

	processPumpEnvVars(i, i.log, i.dbConf, influxDefaultENV)

	i.connect()

	i.log.Debug("Influx DB CS: ", i.dbConf.Addr)
	i.log.Info(i.GetName() + " Initialized")

	return nil
}

func (i *InfluxPump) connect() client.Client {
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     i.dbConf.Addr,
		Username: i.dbConf.Username,
		Password: i.dbConf.Password,
	})

	if err != nil {
		i.log.Error("Influx connection failed:", err)
		time.Sleep(5 * time.Second)
		i.connect()
	}

	return c
}

func (i *InfluxPump) WriteData(ctx context.Context, data []interface{}) error {
	c := i.connect()
	defer c.Close()
	i.log.Debug("Attempting to write ", len(data), " records...")

	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  i.dbConf.DatabaseName,
		Precision: "us",
	})

	var pt *client.Point
	var err error

	//	 Create a point and add to batch
	for _, v := range data {
		// Convert to AnalyticsRecord
		decoded := v.(analytics.AnalyticsRecord)
		mapping := map[string]interface{}{
			"method":        decoded.Method,
			"path":          decoded.Path,
			"response_code": decoded.ResponseCode,
			"api_key":       decoded.APIKey,
			"time_stamp":    decoded.TimeStamp,
			"api_version":   decoded.APIVersion,
			"api_name":      decoded.APIName,
			"api_id":        decoded.APIID,
			"org_id":        decoded.OrgID,
			"oauth_id":      decoded.OauthID,
			"raw_request":   decoded.RawRequest,
			"request_time":  decoded.RequestTime,
			"raw_response":  decoded.RawResponse,
			"ip_address":    decoded.IPAddress,
		}

		tags := make(map[string]string)
		fields := make(map[string]interface{})

		// Select tags from config
		for _, t := range i.dbConf.Tags {
			var tag string

			b, err := json.Marshal(mapping[t])
			if err != nil {
				tag = ""
			} else {

				// convert and remove surrounding quotes from tag value
				tag = strings.Trim(string(b), "\"")
			}
			tags[t] = tag
		}

		// Select field from config
		for _, f := range i.dbConf.Fields {
			fields[f] = mapping[f]
		}

		// New record
		if pt, err = client.NewPoint(table, tags, fields, time.Now()); err != nil {
			i.log.Error(err)
			continue
		}

		// Add point to batch point
		bp.AddPoint(pt)
	}

	// Now that all points are added, write the batch
	c.Write(bp)
	i.log.Info("Purged ", len(data), " records...")

	return nil
}
