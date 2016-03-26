package mysqlfe

import (
	"sync"

	u "github.com/araddon/gou"

	"github.com/araddon/qlbridge/expr"
	"github.com/araddon/qlbridge/value"
)

var _ = u.EMPTY
var loadOnce sync.Once

func init() {
	LoadMySqlFunctions()
}
func LoadMySqlFunctions() {
	loadOnce.Do(func() {
		expr.FuncAdd("current_user", CurrentUser)
		expr.FuncAdd("connection_id", ConnectionId)
		expr.FuncAdd("database", DatabaseName)
	})
}

// DatabaseName:   name of the database
//
//      DATABASE()     =>  "your_db", true
//
func DatabaseName(ctx expr.EvalContext) (value.StringValue, bool) {
	if dbVal, ok := ctx.Get("@@database"); ok {
		if dbValStr, ok := dbVal.(value.StringValue); ok {
			return dbValStr, true
		}
	}
	//u.WarnT(10)
	u.Warnf("database: %#v", ctx)
	return value.NewStringValue(""), true
}

// ConnectionId:   id of current connection
//
//      connection_id()     =>  11, true
//
func ConnectionId(ctx expr.EvalContext) (value.IntValue, bool) {
	if connVal, ok := ctx.Get("@@connection_id"); ok {
		if connInt, ok := connVal.(value.IntValue); ok {
			return connInt, true
		}
	}
	//u.Infof("ConnectionId: %#v", ctx)
	return value.NewIntValue(1), true
}

// CurrentUser:   username of current user
//
//      current_user()     =>  user, true
//
func CurrentUser(ctx expr.EvalContext) (value.StringValue, bool) {
	// switch node := val.(type) {
	// case value.StringValue:
	// 	return value.NewIntValue(int64(len(node.Val()))), true
	// }
	u.Infof("CurrentUser: %#v", ctx)
	return value.NewStringValue("root"), true
}
