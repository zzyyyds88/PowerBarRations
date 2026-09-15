package legacy

import (
	"database/sql"
	"fmt"

	_ "github.com/glebarez/sqlite"
)

// 本包实现 docs/design-v1.md §11 的旧两层网关迁移：路由层（octopus）+ 厂商层（new-api）
// 读入只读 SQLite 副本，按 §11.1 算法产出 PBR 的渠道/车道/客户端密钥，并输出对账报告。
//
// 安全边界：只读旧库副本；真实渠道名/地址/key 只写入目标 PBR 库与本地报告，
// 不进仓库、不进日志、不进 commit（报告默认写到仓库外路径）。

// RoutingChannel 是 octopus 路由层的厂商渠道。
type RoutingChannel struct {
	ID      int
	Name    string
	Type    string
	Enabled bool
	BaseURL string
	Key     string
}

// RoutingChannelModel 是 octopus 渠道声明提供的上游模型名（发给厂商层的请求名 M）。
type RoutingChannelModel struct {
	ID        int
	ChannelID int
	Name      string
	Source    string
}

// RoutingGroup 是 octopus 的车道（group）。
type RoutingGroup struct {
	ID           int
	Name         string
	Mode         string
	ActiveItemID int
}

// RoutingGroupItem 是车道成员：group → channel_model → priority。
type RoutingGroupItem struct {
	ID             int
	GroupID        int
	ChannelModelID int
	Priority       int
}

// RoutingAPIKey 是 octopus 面向下游的客户端密钥。
type RoutingAPIKey struct {
	ID              int
	Name            string
	Key             string
	Enabled         bool
	ExpireAt        int64
	SupportedModels string
}

// RoutingData 是路由层读入结果。
type RoutingData struct {
	Channels      []RoutingChannel
	ChannelModels []RoutingChannelModel
	Groups        []RoutingGroup
	GroupItems    []RoutingGroupItem
	APIKeys       []RoutingAPIKey
}

// VendorChannel 是 new-api 厂商层渠道（持有真实 key 与 model_mapping）。
type VendorChannel struct {
	ID           int
	Type         int
	Key          string
	Status       int
	Name         string
	Weight       int
	BaseURL      string
	Models       string
	Group        string
	ModelMapping string
	Priority     int
}

// VendorAbility 是厂商层能力表（group×model→channel）。
type VendorAbility struct {
	Group     string
	Model     string
	ChannelID int
	Enabled   bool
	Priority  int
	Weight    int
}

// VendorToken 是 new-api 的存量下游令牌。
type VendorToken struct {
	ID                 int
	UserID             int
	Key                string
	Status             int
	Name               string
	ExpiredTime        int64
	ModelLimitsEnabled bool
	ModelLimits        string
	AllowIPs           string
	Group              string
	Deleted            bool
}

// VendorData 是厂商层读入结果。
type VendorData struct {
	Channels  []VendorChannel
	Abilities []VendorAbility
	Tokens    []VendorToken
}

func openRO(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("db path is required")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, err
	}
	return db, nil
}

func each(db *sql.DB, query string, fn func(*sql.Rows) error) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ReadRouting 读入 octopus 路由层。
func ReadRouting(path string) (*RoutingData, error) {
	db, err := openRO(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := &RoutingData{}

	if err := each(db, "SELECT id, COALESCE(name,''), COALESCE(type,''), COALESCE(enabled,1), COALESCE(base_url,''), COALESCE(key,'') FROM channels", func(rows *sql.Rows) error {
		var r RoutingChannel
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.Enabled, &r.BaseURL, &r.Key); err != nil {
			return err
		}
		out.Channels = append(out.Channels, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read octopus channels: %w", err)
	}

	if err := each(db, "SELECT id, channel_id, COALESCE(name,''), COALESCE(source,'') FROM channel_models", func(rows *sql.Rows) error {
		var r RoutingChannelModel
		if err := rows.Scan(&r.ID, &r.ChannelID, &r.Name, &r.Source); err != nil {
			return err
		}
		out.ChannelModels = append(out.ChannelModels, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read octopus channel_models: %w", err)
	}

	if err := each(db, "SELECT id, COALESCE(name,''), COALESCE(mode,'failover'), COALESCE(active_item_id,0) FROM groups", func(rows *sql.Rows) error {
		var r RoutingGroup
		if err := rows.Scan(&r.ID, &r.Name, &r.Mode, &r.ActiveItemID); err != nil {
			return err
		}
		out.Groups = append(out.Groups, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read octopus groups: %w", err)
	}

	if err := each(db, "SELECT id, group_id, channel_model_id, priority FROM group_items", func(rows *sql.Rows) error {
		var r RoutingGroupItem
		if err := rows.Scan(&r.ID, &r.GroupID, &r.ChannelModelID, &r.Priority); err != nil {
			return err
		}
		out.GroupItems = append(out.GroupItems, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read octopus group_items: %w", err)
	}

	if err := each(db, "SELECT id, COALESCE(name,''), COALESCE(api_key,''), COALESCE(enabled,1), COALESCE(expire_at,0), COALESCE(supported_models,'') FROM api_keys", func(rows *sql.Rows) error {
		var r RoutingAPIKey
		if err := rows.Scan(&r.ID, &r.Name, &r.Key, &r.Enabled, &r.ExpireAt, &r.SupportedModels); err != nil {
			return err
		}
		out.APIKeys = append(out.APIKeys, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read octopus api_keys: %w", err)
	}

	return out, nil
}

// ReadVendor 读入 new-api 厂商层。
func ReadVendor(path string) (*VendorData, error) {
	db, err := openRO(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := &VendorData{}

	if err := each(db, "SELECT id, COALESCE(type,0), COALESCE(key,''), COALESCE(status,1), COALESCE(name,''), COALESCE(weight,0), COALESCE(base_url,''), COALESCE(models,''), COALESCE([group],''), COALESCE(model_mapping,''), COALESCE(priority,0) FROM channels", func(rows *sql.Rows) error {
		var r VendorChannel
		if err := rows.Scan(&r.ID, &r.Type, &r.Key, &r.Status, &r.Name, &r.Weight, &r.BaseURL, &r.Models, &r.Group, &r.ModelMapping, &r.Priority); err != nil {
			return err
		}
		out.Channels = append(out.Channels, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read new-api channels: %w", err)
	}

	if err := each(db, "SELECT COALESCE([group],''), model, channel_id, COALESCE(enabled,1), COALESCE(priority,0), COALESCE(weight,0) FROM abilities", func(rows *sql.Rows) error {
		var r VendorAbility
		if err := rows.Scan(&r.Group, &r.Model, &r.ChannelID, &r.Enabled, &r.Priority, &r.Weight); err != nil {
			return err
		}
		out.Abilities = append(out.Abilities, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read new-api abilities: %w", err)
	}

	if err := each(db, "SELECT id, COALESCE(user_id,0), COALESCE(key,''), COALESCE(status,1), COALESCE(name,''), COALESCE(expired_time,-1), COALESCE(model_limits_enabled,0), COALESCE(model_limits,''), COALESCE(allow_ips,''), COALESCE([group],''), CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END FROM tokens", func(rows *sql.Rows) error {
		var r VendorToken
		var deleted int
		if err := rows.Scan(&r.ID, &r.UserID, &r.Key, &r.Status, &r.Name, &r.ExpiredTime, &r.ModelLimitsEnabled, &r.ModelLimits, &r.AllowIPs, &r.Group, &deleted); err != nil {
			return err
		}
		r.Deleted = deleted != 0
		out.Tokens = append(out.Tokens, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read new-api tokens: %w", err)
	}

	return out, nil
}
