package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 审计导出查询（api-spec §5.5.2）的模型层契约：区间闭端点、action/resource/name
// 精确过滤、计数与列表同口径、id 倒序与游标分页。
//
// 测试名含 Export，保证 `go test ./model -run 'Export'` 能覆盖本文件的查询面。
func TestExportAuditFilterQuery(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM pbr_audit_logs").Error)

	base := int64(1_767_225_600) // 2026-01-01T00:00:00Z
	rows := []PBRAuditLog{
		{
			Ts:       base + 10,
			Actor:    "admin",
			Action:   "create",
			Resource: "channel",
			Name:     "ch-a",
		},
		{
			Ts:       base + 20,
			Actor:    "admin",
			Action:   "update",
			Resource: "client_key",
			Name:     "key-a",
		},
		{
			Ts:       base + 30,
			Actor:    "admin",
			Action:   "create",
			Resource: "channel",
			Name:     "ch-b",
		},
		// 区间外（超过 31 天）：验证 Until 闭端点过滤。
		{
			Ts:       base + 40*24*60*60,
			Actor:    "admin",
			Action:   "create",
			Resource: "channel",
			Name:     "ch-outside",
		},
	}
	for i := range rows {
		require.NoError(t, DB.Create(&rows[i]).Error)
	}

	cases := []struct {
		name   string
		filter PBRAuditFilter
		want   int64
	}{
		{
			name:   "window is inclusive on both ends",
			filter: PBRAuditFilter{Since: base + 10, Until: base + 30},
			want:   3,
		},
		{
			name:   "action filter",
			filter: PBRAuditFilter{Action: "create"},
			want:   3,
		},
		{
			name:   "resource filter",
			filter: PBRAuditFilter{Resource: "client_key"},
			want:   1,
		},
		{
			name:   "name filter is exact",
			filter: PBRAuditFilter{Name: "ch-a"},
			want:   1,
		},
		{
			name:   "name filter does not prefix match",
			filter: PBRAuditFilter{Name: "ch-"},
			want:   0,
		},
		{
			name:   "combined filters",
			filter: PBRAuditFilter{Action: "create", Resource: "channel", Since: base, Until: base + 20},
			want:   1,
		},
		{
			name:   "no match",
			filter: PBRAuditFilter{Action: "rotate"},
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CountPBRAudits(tc.filter)
			require.NoError(t, err)
			assert.EqualValues(t, tc.want, got)

			entries, err := ListPBRAuditsFiltered(tc.filter)
			require.NoError(t, err)
			assert.Len(t, entries, int(tc.want), "列表与计数的过滤口径必须一致")
		})
	}

	// id 倒序：最近写入的行在前。
	entries, err := ListPBRAuditsFiltered(PBRAuditFilter{Since: base, Until: base + 30})
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, "ch-b", entries[0].Name)
	assert.Equal(t, "key-a", entries[1].Name)
	assert.Equal(t, "ch-a", entries[2].Name)

	// 游标分页：Limit 后以最后一行 id 作为 BeforeId 继续。
	first, err := ListPBRAuditsFiltered(PBRAuditFilter{Since: base, Until: base + 30, Limit: 2})
	require.NoError(t, err)
	require.Len(t, first, 2)
	second, err := ListPBRAuditsFiltered(PBRAuditFilter{
		Since: base, Until: base + 30, Limit: 2, BeforeId: first[len(first)-1].Id,
	})
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, "ch-a", second[0].Name)
	assert.NotEqual(t, first[0].Id, second[0].Id)
}
