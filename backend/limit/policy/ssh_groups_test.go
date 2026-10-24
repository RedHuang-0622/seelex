package policy

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestSSHIsOneRemoteCapability(t *testing.T) {
	groups := DefaultPermissionGroupList()
	bits, ok := RemoteFaceGroupBits(groups, GroupSSH)
	if !ok || bits != BitExecute {
		t.Fatalf("ssh 位 = %d ok=%v，want x", bits, ok)
	}
	count := 0
	for _, group := range groups {
		if group.Name == GroupSSH {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ssh 组数量 = %d，want 1（不得再拆 ssh.userspace）", count)
	}
	if _, exists := RemoteFaceGroupBits(groups, "ssh.userspace"); exists {
		t.Fatal("旧 ssh.userspace 不得继续作为独立权限组")
	}
}

func TestSSHVectorTagAndLegacyAliasMapToOneCapability(t *testing.T) {
	tags := TagsForGroups(map[string]uint8{GroupRO: BitRead, GroupSSH: BitExecute})
	found := false
	for _, tag := range tags {
		if tag == dto.PermissionGroupSSH {
			found = true
		}
		if tag == "ssh.userspace" {
			t.Fatal("ssh.userspace 不得再出现在新向量里")
		}
	}
	if !found {
		t.Fatalf("tags = %v，缺 ssh", tags)
	}
	if got := dto.CanonicalPermissionGroup("ssh.userspace"); got != dto.PermissionGroupSSH {
		t.Fatalf("legacy alias = %q", got)
	}
}

func TestSSHIsAvailableAcrossTierPresets(t *testing.T) {
	groups := DefaultPermissionGroupList()
	for _, tier := range dto.PermissionTierIDs() {
		if !TierCells(tier, groups).Covers(GroupSSH, 0) {
			t.Errorf("tier %s 缺 ssh 能力", tier)
		}
	}
	for subject, grant := range DefaultPermissionSubjects() {
		if grant.Bits[GroupSSH].Bits&BitExecute == 0 {
			t.Errorf("subject %s 缺 ssh 执行位", subject)
		}
		if _, exists := grant.Bits["ssh.userspace"]; exists {
			t.Errorf("subject %s 仍保留旧 ssh.userspace 格", subject)
		}
	}
}
