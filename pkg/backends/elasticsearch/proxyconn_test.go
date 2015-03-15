package elasticsearch

import (
	"sync"
	"testing"
	"time"

	u "github.com/araddon/gou"
	"github.com/bmizerany/assert"
	"github.com/dataux/dataux/pkg/models"
	"github.com/dataux/dataux/vendor/mixer/client"
	mysqlproxy "github.com/dataux/dataux/vendor/mixer/proxy"
)

var (
	_              = u.EMPTY
	testServerOnce sync.Once
	testListener   *TestListenerWraper
	testDBOnce     sync.Once
	testDB         *client.DB
)

func init() {
	// We need to register our DataSource provider here
	models.DataSourceRegister("elasticsearch", NewElasticsearchDataSource)
}

var testConfigData = `

supress_recover: true

frontends [
  {
    name : mysql 
    type : "mysql"
    addr : "127.0.0.1:4000"
    user : root
  }
]

sources [
  {
    name : node1
    address : "http://localhost:9200"
  }
]

schemas : [
  {
    db : es
    source_nodes : ["node1"]
    source_type : elasticsearch
    address : "http://localhost:9200"
  }
]
`

type TestListenerWraper struct {
	*mysqlproxy.MysqlListener
}

func NewTestServer(t *testing.T) *TestListenerWraper {
	f := func() {
		conf, err := models.LoadConfig(testConfigData)
		assert.Tf(t, err == nil, "must load config without err: %v", err)

		svr := models.NewServerCtx(conf)
		svr.Init()

		handler, err := NewMySqlHandler(svr)
		assert.Tf(t, err == nil, "must create es handler without err: %v", err)

		// Load our Frontend Listener's
		models.ListenerRegister(mysqlproxy.ListenerType,
			mysqlproxy.ListenerInit,
			handler,
		)

		myl, err := mysqlproxy.NewMysqlListener(conf.Frontends[0], conf)
		assert.Tf(t, err == nil, "must create listener without err: %v", err)

		testListener = &TestListenerWraper{myl}

		go testListener.Run(handler, make(chan bool))

		// delay to ensure we have time to connect
		time.Sleep(100 * time.Millisecond)
	}

	testServerOnce.Do(f)

	return testListener
}

func RunTestServer(t *testing.T) {
	NewTestServer(t)
}
