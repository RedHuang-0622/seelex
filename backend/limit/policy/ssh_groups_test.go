package policy

// ssh_groups_test.go —— 远端两格（`ssh` / `ssh.userspace`）的词表判据。
//
// 三件事各钉一条：
//
//	1. 组表是**唯一事实源**：DeclaredToolMeta 与 TagsForGroups 自动跟随，没有第二张表；
//	2. 这两格**不接管名字路由**（LMRW 由 ro/rw 胜出），但声明了"远端面工具清单"；
//	3. 位与默认：`ssh` = x（通道），`ssh.userspace` = r|w|x（越界），档位里前者默认给、
//	   后者只有 full 给。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestSSHGroupsAreDeclaredInTheTable：两组在表里、位与 Match 是口径本身。
func TestSSHGroupsAreDeclaredInTheTable(t *testing.T) {
	groups := DefaultPermissionGroupList()
	channel, ok := RemoteFaceGroupBits(groups, GroupSSH)
	if !ok {
		t.Fatalf("组表里没有 %s（通道位是这道闸门的一半）", GroupSSH)
	}
	if channel != BitExecute {
		t.Fatalf("%s 的位 = %d，want %d（x = 有这条路）", GroupSSH, channel, BitExecute)
	}
	userspace, ok := RemoteFaceGroupBits(groups, GroupSSHUserspace)
	if !ok {
		t.Fatalf("组表里没有 %s（越界位是这道闸门的另一半）", GroupSSHUserspace)
	}
	if userspace != BitRead|BitWrite|BitExecute {
		t.Fatalf("%s 的位 = %d，want r|w|x = %d", GroupSSHUserspace, userspace, BitRead|BitWrite|BitExecute)
	}
}

// TestRemoteFaceRosterComesFromTheGroupTable：远端面工具清单**派生**自 ssh 组的 Match。
//
// 这条钉住"不许另建第二张表"：registry 的 RemoteFaceToolNames() 就是读这里，
// 名单写错/漏写会让"选了 ssh 却跑本地"重新变成可能。
func TestRemoteFaceRosterComesFromTheGroupTable(t *testing.T) {
	roster := RemoteFaceToolsFromGroups(DefaultPermissionGroupList())
	want := []string{"read_file", "write_file", "edit_file", "bash", "bg_manage"}
	if len(roster) != len(want) {
		t.Fatalf("远端面清单 = %v，want %v", roster, want)
	}
	for index := range want {
		if roster[index] != want[index] {
			t.Fatalf("远端面清单 = %v，want %v", roster, want)
		}
	}
	// 不存在的组 → nil（不猜）；glob 形态被跳过（清单要的是具体工具名）。
	if got := RemoteFaceToolsFromGroups(nil); got != nil {
		t.Fatalf("空组表的远端面清单 = %v，want nil（不猜）", got)
	}
}

// TestSSHGroupsDoNotTakeOverNameRouting：声明了名字，但**不改路由**（LMRW 由操作类型组胜出）。
//
// 这是"本地执行面行为逐字不变"的前提：远端面与操作类型正交，名字里没有"在哪台机器"
// 这个信息；按面判定只能发生在已定的执行面上。
func TestSSHGroupsDoNotTakeOverNameRouting(t *testing.T) {
	groups := DefaultPermissionGroupList()
	for _, name := range RemoteFaceToolsFromGroups(groups) {
		group, routed := RoutePermissionGroup(groups, name)
		if !routed {
			t.Fatalf("%s 未分封（应命中 ro/rw）", name)
		}
		if group.Name == GroupSSH || group.Name == GroupSSHUserspace {
			t.Fatalf("%s 路由到了 %q——按面判定的组不得接管名字路由", name, group.Name)
		}
		// 簇属派生（DeclaredToolMeta）读的是同一个路由，因此也必须落在操作类型组上。
		if declared := DeclaredToolMeta(name); len(declared.Groups) != 1 ||
			(declared.Groups[0] != GroupRO && declared.Groups[0] != GroupRW) {
			t.Fatalf("%s 的声明簇属 = %+v，want ro/rw（自动跟随组表，不另建表）", name, declared)
		}
	}
}

// TestTagsForGroupsFollowsTheVocabulary：向量投影自动跟随词表（每组都要表态）。
func TestTagsForGroupsFollowsTheVocabulary(t *testing.T) {
	tags := TagsForGroups(map[string]uint8{GroupRO: BitRead, GroupSSH: BitExecute})
	allowed := map[string]bool{}
	denied := map[string]bool{}
	for _, tag := range tags {
		if len(tag) > 0 && tag[0] == dto.PermissionTagDenyPrefix[0] {
			denied[tag[1:]] = true
			continue
		}
		allowed[tag] = true
	}
	if len(tags) != len(dto.PermissionGroupNames()) {
		t.Fatalf("标签数 = %d，want %d（规范组序每组一格）：%v", len(tags), len(dto.PermissionGroupNames()), tags)
	}
	if !allowed[GroupSSH] || allowed[GroupSSHUserspace] {
		t.Fatalf("投影 = %v，want ssh 给、ssh.userspace 显式不给", tags)
	}
	if !denied[GroupSSHUserspace] {
		t.Fatalf("ssh.userspace 位为 0 必须投影成 `!ssh.userspace`（沉默会被四层解析读成允许）：%v", tags)
	}
}

// TestTierGrantsChannelButNotUserspace：档位预设里通道位四档全给、越界位只有 full 给。
func TestTierGrantsChannelButNotUserspace(t *testing.T) {
	groups := DefaultPermissionGroupList()
	cases := []struct {
		tier      string
		userspace bool
	}{
		{dto.PermissionTierManual, false},
		{dto.PermissionTierEdit, false},
		{dto.PermissionTierAuto, false},
		{dto.PermissionTierFull, true},
	}
	for _, expect := range cases {
		cells := TierCells(expect.tier, groups)
		if !cells.Covers(GroupSSH, 0) {
			t.Errorf("档位 %s 没有 ssh 通道位（远端面会整体不可达）", expect.tier)
		}
		if got := cells.Covers(GroupSSHUserspace, 0); got != expect.userspace {
			t.Errorf("档位 %s 的 ssh.userspace = %v，want %v", expect.tier, got, expect.userspace)
		}
	}
}

// TestDefaultSubjectsDoNotHandOutUserspace：默认主体表谁都不给越界位（闸门默认关着）。
func TestDefaultSubjectsDoNotHandOutUserspace(t *testing.T) {
	for subject, grant := range DefaultPermissionSubjects() {
		if bits := grant.Bits[GroupSSHUserspace].Bits; bits != 0 && subject != "root" {
			t.Errorf("主体 %s 默认拿到 ssh.userspace 位 = %d，want 0（越界必须显式授予）", subject, bits)
		}
		if bits := grant.Bits[GroupSSH].Bits; bits&BitExecute == 0 {
			t.Errorf("主体 %s 默认没有 ssh 通道位（远端面在本工作项之前就可达，不该被顺手收窄）", subject)
		}
	}
}
