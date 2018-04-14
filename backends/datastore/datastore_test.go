package datastore_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/datastore"
	u "github.com/araddon/gou"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"

	"github.com/araddon/qlbridge/datasource"
	"github.com/araddon/qlbridge/plan"
	"github.com/araddon/qlbridge/schema"

	"github.com/dataux/dataux/frontends/mysqlfe/testmysql"
	"github.com/dataux/dataux/planner"
	tu "github.com/dataux/dataux/testutil"
)

var (
	ctx                 context.Context
	client              *datastore.Client
	DbConn              = "root@tcp(127.0.0.1:13307)/datauxtest?parseTime=true"
	loadTestDataOnce    sync.Once
	skip                = false
	now                 = time.Now()
	testServicesRunning bool
	SchemaName          = "datauxtest"
)

func init() {

	tu.Setup()

	// export DATASTORE_EMULATOR_HOST="localhost:8432"
	if addr := os.Getenv("DATASTORE_EMULATOR_HOST"); addr == "" {
		//println("datastore_test.go setting datastore emulator")
		os.Setenv("DATASTORE_EMULATOR_HOST", "localhost:8432")
		u.Infof("setting datastore emulator  %v", os.Getenv("DATASTORE_EMULATOR_HOST"))
	}
	ctx, client = loadEmulatorClient()
}

func loadEmulatorClient() (context.Context, *datastore.Client) {
	ctx := context.Background()
	client, err := datastore.NewClient(ctx, "lol")
	if err != nil {
		panic(fmt.Sprintf("could not create google datastore client: err=%v", err))
	}
	return ctx, client
}

func loadJWTAuth(jsonKey []byte, projectId string) (context.Context, *datastore.Client) {
	// Initialize an authorized context with Google Developers Console
	// JSON key. Read the google package examples to learn more about
	// different authorization flows you can use.
	// http://godoc.org/golang.org/x/oauth2/google
	conf, err := google.JWTConfigFromJSON(
		jsonKey,
		datastore.ScopeDatastore,
	)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	client, err := datastore.NewClient(ctx, projectId, option.WithTokenSource(conf.TokenSource(ctx)))
	if err != nil {
		panic(err.Error())
	}
	return ctx, client
}

func jobMaker(ctx *plan.Context) (*planner.GridTask, error) {
	ctx.Schema = testmysql.Schema
	return planner.BuildSqlJob(ctx, testmysql.ServerCtx.PlanGrid)
}

func RunTestServer(t *testing.T) func() {
	if !testServicesRunning {
		if skip {
			t.Skip("Skipping, provide google JWT tokens if you want to test")
		}

		/*
		  # google-datastore database config
		  {
		    name      : google_ds_test
		    type      : google-datastore
		    settings {
		      projectid : lol
		    }
		  }
		*/

		reg := schema.DefaultRegistry()
		by := []byte(`{
            "name": "google_ds_test",
            "schema":"datauxtest",
            "type": "google-datastore",
             "settings" : {
                "project": "lol"
            }
        }`)

		conf := &schema.ConfigSource{}
		err := json.Unmarshal(by, conf)
		assert.Equal(t, nil, err)
		err = reg.SchemaAddFromConfig(conf)
		assert.Equal(t, nil, err)

		s, ok := reg.Schema("datauxtest")
		assert.Equal(t, true, ok)
		assert.NotEqual(t, nil, s)

		loadTestData(t)

		testServicesRunning = true
		planner.GridConf.JobMaker = jobMaker
		planner.GridConf.SchemaLoader = testmysql.SchemaLoader
		planner.GridConf.SupressRecover = testmysql.Conf.SupressRecover
		testmysql.RunTestServer(t)
	}
	return func() {
		// placeholder
	}
}

func validateQuerySpec(t *testing.T, testSpec tu.QuerySpec) {
	RunTestServer(t)
	tu.ValidateQuerySpec(t, testSpec)
}

func loadTestData(t *testing.T) {
	if skip {
		t.Skip("Skipping, provide JWT tokens in ENV if desired")
	}
	loadTestDataOnce.Do(func() {

		for _, article := range tu.Articles {
			key, err := client.Put(ctx, articleKey(article.Title), &Article{article})
			u.Infof("key: %v", key)
			assert.True(t, key != nil, "%v", key)
			assert.True(t, err == nil, "must put %v", err)
		}
		for i := 0; i < -1; i++ {
			n := time.Now()
			ev := struct {
				Tag string
				ICt int
			}{"tag", i}
			body := json.RawMessage([]byte(fmt.Sprintf(`{"name":"more %v"}`, i)))
			a := &tu.Article{fmt.Sprintf("article_%v", i), "auto", 22, 75, false, []string{"news", "sports"}, n, &n, 55.5, ev, &body}
			key, err := client.Put(ctx, articleKey(a.Title), &Article{a})
			//u.Infof("key: %v", key)
			assert.True(t, key != nil, "%v", key)
			assert.True(t, err == nil, "must put %v", err)
			//u.Warnf("made article: %v", a.Title)
		}
		for _, user := range tu.Users {
			key, err := client.Put(ctx, userKey(user.Id), &User{user})
			//u.Infof("key: %v", key)
			assert.True(t, err == nil, "must put %v", err)
			assert.True(t, key != nil, "%v", key)
		}
	})
}

// We are testing that we can register this Google Datastore Source
// as a qlbridge Source
func TestSourceInterface(t *testing.T) {

	RunTestServer(t)

	// Now make sure that the datastore source has been registered
	// and meets api for qlbridge.DataSource
	ds, err := schema.OpenConn(SchemaName, ArticleKind)
	assert.Equal(t, nil, err)
	assert.NotEqual(t, nil, ds)
}

func TestInvalidQuery(t *testing.T) {

	RunTestServer(t)

	db, err := sql.Open("mysql", DbConn)
	assert.Equal(t, nil, err)
	// It is parsing the SQL on server side (proxy) not in client
	// so hence that is what this is testing, making sure proxy responds gracefully
	rows, err := db.Query("select `stuff`, NOTAKEYWORD fake_tablename NOTWHERE `description` LIKE \"database\";")
	assert.NotEqual(t, nil, err)
	assert.True(t, nil == rows, "must not get rows")
}

func TestShowTables(t *testing.T) {

	RunTestServer(t)

	data := struct {
		Table string `db:"Table"`
	}{}
	found := false
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         "show tables;",
		ExpectRowCt: -1,
		ValidateRowData: func() {
			u.Infof("%v", data)
			assert.True(t, data.Table != "", "%v", data)
			if data.Table == strings.ToLower(ArticleKind) {
				found = true
			}
		},
		RowData: &data,
	})
	assert.True(t, found, "Must have found %s", ArticleKind)
}

func TestBasic(t *testing.T) {

	RunTestServer(t)

	// This is a connection to RunTestServer, which starts on port 13307
	dbx, err := sqlx.Connect("mysql", DbConn)
	assert.Equal(t, nil, err)
	defer dbx.Close()
	rows, err := dbx.Queryx(fmt.Sprintf("select * from %s", ArticleKind))
	assert.Equal(t, nil, err)
	defer rows.Close()

	/*
		aidAlias := fmt.Sprintf("%d-%s", 123, "query_users1")
		key := datastore.NewKey(ctx, QueryKind, aidAlias, 0, nil)
		qd := &QueryData{}
		err := datastore.Get(ctx, key, qd)
		assert.True(t, err == nil, "no error: %v", err)
		assert.True(t, qd.Alias == "query_users1", "has alias")
		queryMeta()
	*/
}

func TestDescribeTable(t *testing.T) {

	RunTestServer(t)

	data := struct {
		Field   string `db:"Field"`
		Type    string `db:"Type"`
		Null    string `db:"Null"`
		Key     string `db:"Key"`
		Default string `db:"Default"`
		Extra   string `db:"Extra"`
	}{}
	describedCt := 0
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         fmt.Sprintf("describe %s;", ArticleKind),
		ExpectRowCt: 11,
		ValidateRowData: func() {
			//u.Infof("%s   %#v", data.Field, data)
			assert.True(t, data.Field != "", "%v", data)
			switch data.Field {
			case "embedded":
				assert.True(t, data.Type == "text", "%#v", data)
				describedCt++
			case "author":
				assert.True(t, data.Type == "varchar(255)", "data: %#v", data)
				describedCt++
			case "created":
				assert.True(t, data.Type == "datetime", "data: %#v", data)
				describedCt++
			case "category":
				assert.True(t, data.Type == "text", "data: %#v", data)
				describedCt++
			case "body":
				assert.True(t, data.Type == "text", "data: %#v", data)
				describedCt++
			case "deleted":
				assert.True(t, data.Type == "tinyint", "data: %#v", data)
				describedCt++
			}
		},
		RowData: &data,
	})
	assert.True(t, describedCt == 6, "Should have found/described 6 but was %v", describedCt)
}

func TestSimpleRowSelect(t *testing.T) {

	RunTestServer(t)

	data := struct {
		Title   string
		Count   int
		Deleted bool
		Author  string
		// Category []string  // Crap, downside of sqlx/mysql is no complex types
	}{}
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         "select title, count, deleted, author from DataUxTestArticle WHERE author = \"aaron\" LIMIT 1",
		ExpectRowCt: 1,
		ValidateRowData: func() {
			//u.Infof("%v", data)
			assert.True(t, data.Deleted == false, "Not deleted? %v", data)
			assert.True(t, data.Title == "article1", "%v", data)
		},
		RowData: &data,
	})
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         "select title, count,deleted from DataUxTestArticle WHERE count = 22;",
		ExpectRowCt: 1,
		ValidateRowData: func() {
			assert.True(t, data.Title == "article1", "%v", data)
		},
		RowData: &data,
	})
	validateQuerySpec(t, tu.QuerySpec{
		Sql:             "select title, count, deleted from DataUxTestArticle LIMIT 10;",
		ExpectRowCt:     4,
		ValidateRowData: func() {},
		RowData:         &data,
	})
}

func TestSelectLimit(t *testing.T) {
	data := struct {
		Title string
		Count int
	}{}
	validateQuerySpec(t, tu.QuerySpec{
		Sql:             "select title, count from DataUxTestArticle LIMIT 1;",
		ExpectRowCt:     1,
		ValidateRowData: func() {},
		RowData:         &data,
	})
}

func TestSelectWhereLike(t *testing.T) {
	data := struct {
		Title string
		Ct    int
	}{}
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         `SELECT title, count as ct from DataUxTestArticle WHERE title like "list%"`,
		ExpectRowCt: 1,
		ValidateRowData: func() {
			assert.True(t, data.Title == "listicle1", "%v", data)
		},
		RowData: &data,
	})
	// TODO:  poly fill this, as doesn't work in datastore
	// validateQuerySpec(t, tu.QuerySpec{
	// 	Sql:         `SELECT title, count as ct from article WHERE title like "%stic%"`,
	// 	ExpectRowCt: 1,
	// 	ValidateRowData: func() {
	// 		assert.True(t, data.Title == "listicle1", "%v", data)
	// 	},
	// 	RowData: &data,
	// })
}

func TestSelectOrderBy(t *testing.T) {
	data := struct {
		Title string
		Ct    int
	}{}
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         "select title, count64 AS ct FROM DataUxTestArticle ORDER BY count64 DESC LIMIT 1;",
		ExpectRowCt: 1,
		ValidateRowData: func() {
			assert.True(t, data.Title == "zarticle3", "%v", data)
			assert.True(t, data.Ct == 100, "%v", data)
		},
		RowData: &data,
	})
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         "select title, count64 AS ct FROM DataUxTestArticle ORDER BY count64 ASC LIMIT 1;",
		ExpectRowCt: 1,
		ValidateRowData: func() {
			assert.True(t, data.Title == "listicle1", "%v", data)
			assert.True(t, data.Ct == 12, "%v", data)
		},
		RowData: &data,
	})
}

func TestInsertSimple(t *testing.T) {
	validateQuerySpec(t, tu.QuerySpec{
		Exec:            `INSERT INTO DataUxTestUser (id, name, deleted, created, updated) VALUES ("user814", "test_name",false, now(), now());`,
		ValidateRowData: func() {},
		ExpectRowCt:     1,
	})
}

func TestDeleteSimple(t *testing.T) {
	validateQuerySpec(t, tu.QuerySpec{
		Exec:            `INSERT INTO DataUxTestUser (id, name, deleted, created, updated) VALUES ("user814", "test_name",false, now(), now());`,
		ValidateRowData: func() {},
		ExpectRowCt:     1,
	})
	validateQuerySpec(t, tu.QuerySpec{
		Exec:            `DELETE FROM DataUxTestUser WHERE id = "user814"`,
		ValidateRowData: func() {},
		ExpectRowCt:     1,
	})
	validateQuerySpec(t, tu.QuerySpec{
		Exec:            `SELECT * FROM DataUxTestUser WHERE id = "user814"`,
		ValidateRowData: func() {},
		ExpectRowCt:     0,
	})
}

func TestUpdateSimple(t *testing.T) {
	data := struct {
		Id      string
		Name    string
		Deleted bool
		Roles   datasource.StringArray
		Created time.Time
		Updated time.Time
	}{}
	//u.Warnf("about to insert")
	validateQuerySpec(t, tu.QuerySpec{
		Exec: `INSERT INTO DataUxTestUser 
							(id, name, deleted, created, updated, roles) 
						VALUES 
							("user815", "test_name", false, todate("2014/07/04"), now(), ["admin","sysadmin"]);`,
		ValidateRowData: func() {},
		ExpectRowCt:     1,
	})
	//u.Warnf("about to test post update")
	//return
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         `select id, name, deleted, roles, created, updated from DataUxTestUser WHERE id = "user815"`,
		ExpectRowCt: 1,
		ValidateRowData: func() {
			//u.Infof("%v", data)
			assert.True(t, data.Id == "user815", "%v", data)
			assert.True(t, data.Name == "test_name", "Name: %v", data.Name)
			assert.True(t, data.Deleted == false, "Not deleted? %v", data)
		},
		RowData: &data,
	})
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         `SELECT id, name, deleted, roles, created, updated FROM DataUxTestUser WHERE id = "user815"`,
		ExpectRowCt: 1,
		ValidateRowData: func() {
			u.Infof("%v", data)
			assert.True(t, data.Id == "user815", "%v", data)
			assert.True(t, data.Deleted == false, "Not deleted? %v", data)
		},
		RowData: &data,
	})
	//u.Warnf("about to update")
	validateQuerySpec(t, tu.QuerySpec{
		Exec:            `UPDATE DataUxTestUser SET name = "was_updated", [deleted] = true WHERE id = "user815"`,
		ValidateRowData: func() {},
		ExpectRowCt:     1,
	})
	//u.Warnf("about to final read")
	validateQuerySpec(t, tu.QuerySpec{
		Sql:         `SELECT id, name, deleted, roles, created, updated FROM DataUxTestUser WHERE id = "user815"`,
		ExpectRowCt: 1,
		ValidateRowData: func() {
			u.Infof("%v", data)
			assert.True(t, data.Id == "user815", "fr1 %v", data)
			assert.True(t, data.Name == "was_updated", "fr2 %v", data)
			assert.True(t, data.Deleted == true, "fr3 deleted? %v", data)
		},
		RowData: &data,
	})
	validateQuerySpec(t, tu.QuerySpec{
		Exec:            `DELETE FROM DataUxTestUser WHERE id = "user815"`,
		ValidateRowData: func() {},
		ExpectRowCt:     1,
	})
}

const (
	ArticleKind string = "DataUxTestArticle"
	UserKind    string = "DataUxTestUser"
)

func articleKey(title string) *datastore.Key {
	return datastore.NameKey(ArticleKind, title, nil)
}

func userKey(id string) *datastore.Key {
	return datastore.NameKey(UserKind, id, nil)
}

/*
type Article struct {
	Title    string
	Author   string
	Count    int
	Count64  int64
	Deleted  bool
	Category []string
	Created  time.Time
	Updated  *time.Time
	F        float64
	Embedded struct {
		Tag string
		ICt int
	}
	Body *json.RawMessage
}
*/
type Article struct {
	*tu.Article
}

func NewArticle() Article {
	return Article{&tu.Article{}}
}

func (m *Article) Load(props []datastore.Property) error {
	for _, p := range props {
		switch p.Name {
		default:
			u.Warnf("unmapped: %v  %T", p.Name, p.Value)
		}
	}
	return nil
}
func (m *Article) Save() ([]datastore.Property, error) {
	props := make([]datastore.Property, 11)
	props[0] = datastore.Property{Name: "title", Value: m.Title}
	props[1] = datastore.Property{Name: "author", Value: m.Author}
	props[2] = datastore.Property{Name: "count", Value: m.Count}
	props[3] = datastore.Property{Name: "count64", Value: m.Count64}
	props[4] = datastore.Property{Name: "deleted", Value: m.Deleted}
	cat, _ := json.Marshal(m.Category)
	props[5] = datastore.Property{Name: "category", Value: cat, NoIndex: true}
	props[6] = datastore.Property{Name: "created", Value: m.Created}
	props[7] = datastore.Property{Name: "updated", Value: *m.Updated}
	props[8] = datastore.Property{Name: "f", Value: m.F}
	embed, _ := json.Marshal(m.Embedded)
	props[9] = datastore.Property{Name: "embedded", Value: embed, NoIndex: true}
	if m.Body != nil {
		props[10] = datastore.Property{Name: "body", Value: []byte(*m.Body), NoIndex: true}
	} else {
		props[10] = datastore.Property{Name: "body", Value: []byte{}, NoIndex: true}
	}

	return props, nil
}

/*
type User struct {
	Id      string
	Name    string
	Deleted bool
	Roles   []string
	Created time.Time
	Updated *time.Time
}

*/
type User struct {
	*tu.User
}

func (m *User) Load(props []datastore.Property) error {
	for _, p := range props {
		switch p.Name {
		case "id":
			m.Id = p.Value.(string)
		default:
			u.Warnf("unmapped: %v  %T", p.Name, p.Value)
		}
	}
	return nil
}
func (m *User) Save() ([]datastore.Property, error) {
	props := make([]datastore.Property, 6)
	roles, _ := m.Roles.Value()
	//u.Infof("roles: %T", roles)
	props[0] = datastore.Property{Name: "id", Value: m.Id}                    // Indexed
	props[1] = datastore.Property{Name: "name", Value: m.Id}                  // Indexed
	props[2] = datastore.Property{Name: "deleted", Value: m.Deleted}          // Indexed
	props[3] = datastore.Property{Name: "roles", Value: roles, NoIndex: true} // Not Indexed
	props[4] = datastore.Property{Name: "created", Value: m.Created}          // Indexed
	props[5] = datastore.Property{Name: "updated", Value: *m.Updated}         // Indexed
	return props, nil
}
