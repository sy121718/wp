package unit

import (
	"context"
	"strconv"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
)

func TestDeptCreateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.DeptCreate(ctx, &admindto.DeptCreateReq{
		DeptName: "总公司", DeptCode: "HQ_" + uniq(""), Status: 1, SortOrder: 1,
	})
	wantErr(t, err, "")

	var dept adminmodel.DeptEntity
	if err := e.db.Where("dept_code LIKE ?", "HQ_%").First(&dept).Error; err != nil {
		t.Fatalf("查询部门失败: %v", err)
	}
	if dept.Ancestors != "0" {
		t.Fatalf("顶级部门 ancestors 应为 0: got=%q", dept.Ancestors)
	}
}

func TestDeptCreateChildAncestors(t *testing.T) {
	e := setupEnv(t)

	parentID := deptCreate(t, e, "母部门", "M_"+uniq(""))
	childID := deptCreate(t, e, "子部门", "C_"+uniq(""), parentID)

	var child adminmodel.DeptEntity
	if err := e.db.First(&child, childID).Error; err != nil {
		t.Fatalf("查询子部门失败: %v", err)
	}
	if child.Ancestors != "0,"+uint64Str(parentID) {
		t.Fatalf("子部门 ancestors 不符: got=%q", child.Ancestors)
	}
}

func TestDeptCreateParentNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.DeptCreate(ctx, &admindto.DeptCreateReq{
		DeptName: "孤儿部门", DeptCode: "OR_" + uniq(""), ParentID: 555555,
	})
	wantErr(t, err, adminenums.ErrDeptNotFound)
}

func TestDeptCreateDuplicateCode(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "DUP_" + uniq("")
	deptCreate(t, e, "部门A", code)
	err := e.svc.DeptCreate(ctx, &admindto.DeptCreateReq{DeptName: "部门B", DeptCode: code})
	wantErr(t, err, adminenums.ErrDeptCodeExists)
}

func TestDeptUpdateMoveAncestors(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	deptA := deptCreate(t, e, "A", "A_"+uniq(""))
	deptB := deptCreate(t, e, "B", "B_"+uniq(""))
	deptD := deptCreate(t, e, "D", "D_"+uniq(""), deptA)
	deptE := deptCreate(t, e, "E", "E_"+uniq(""), deptD)

	err := e.svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{
		ID: deptD, ParentID: deptB, DeptName: "D", DeptCode: deptCode(t, e, deptD), Status: 1,
	})
	wantErr(t, err, "")

	var d, ee adminmodel.DeptEntity
	if err := e.db.First(&d, deptD).Error; err != nil {
		t.Fatalf("查询部门失败: %v", err)
	}
	if d.Ancestors != "0,"+uint64Str(deptB) {
		t.Fatalf("D 移动后 ancestors 不符: got=%q", d.Ancestors)
	}
	if err := e.db.First(&ee, deptE).Error; err != nil {
		t.Fatalf("查询孙部门失败: %v", err)
	}
	if ee.Ancestors != "0,"+uint64Str(deptB)+","+uint64Str(deptD) {
		t.Fatalf("E 的 ancestors 应随父更新: got=%q", ee.Ancestors)
	}
}

func TestDeptUpdateMoveAncestorPrefixCollision(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	dept2 := deptCreateWithID(t, e, 2, "ID2", "ID2_"+uniq(""))
	dept5 := deptCreateWithID(t, e, 5, "ID5", "ID5_"+uniq(""))
	dept20 := deptCreateWithID(t, e, 20, "ID20", "ID20_"+uniq(""))
	dept21 := deptCreateWithID(t, e, 21, "ID21", "ID21_"+uniq(""), dept20)
	_ = dept21

	err := e.svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{
		ID: dept2, ParentID: dept5, DeptName: "ID2", DeptCode: deptCode(t, e, dept2), Status: 1,
	})
	wantErr(t, err, "")

	var dept21After adminmodel.DeptEntity
	if err := e.db.First(&dept21After, dept21).Error; err != nil {
		t.Fatalf("查询部门失败: %v", err)
	}
	if dept21After.Ancestors != "0,"+uint64Str(dept20) {
		t.Fatalf("已知缺陷：无关部门 21 的 ancestors 被误改: got=%q want=%q", dept21After.Ancestors, "0,"+uint64Str(dept20))
	}
}

func TestDeptUpdateCircleSelf(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := deptCreate(t, e, "环部门", "CIR_"+uniq(""))
	err := e.svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{
		ID: id, ParentID: id, DeptName: "环部门", DeptCode: deptCode(t, e, id), Status: 1,
	})
	wantErr(t, err, adminenums.ErrDeptCircle)
}

func TestDeptUpdateCircleDescendant(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	p := deptCreate(t, e, "父", "PC_"+uniq(""))
	c := deptCreate(t, e, "子", "CC_"+uniq(""), p)

	err := e.svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{
		ID: p, ParentID: c, DeptName: "父", DeptCode: deptCode(t, e, p), Status: 1,
	})
	wantErr(t, err, adminenums.ErrDeptCircle)
}

func TestDeptDeleteHasChildren(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	p := deptCreate(t, e, "父部门", "DP_"+uniq(""))
	deptCreate(t, e, "子部门", "DC_"+uniq(""), p)

	err := e.svc.DeptDelete(ctx, &admindto.DeptDeleteReq{ID: p})
	wantErr(t, err, adminenums.ErrDeptHasChildren)
}

func TestDeptDeleteHasUsers(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := deptCreate(t, e, "有人部门", "DU_"+uniq(""))
	createAdminDB(t, e.db, uniq("deptuser"), uniq("deptuser")+"@example.com")
	if err := e.db.Model(&adminmodel.AdminEntity{}).
		Where("username LIKE ?", "deptuser%").Update("dept_id", id).Error; err != nil {
		t.Fatalf("分配部门失败: %v", err)
	}

	err := e.svc.DeptDelete(ctx, &admindto.DeptDeleteReq{ID: id})
	wantErr(t, err, adminenums.ErrDeptHasUsers)
}

func TestDeptDeleteSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := deptCreate(t, e, "空部门", "DE_"+uniq(""))
	err := e.svc.DeptDelete(ctx, &admindto.DeptDeleteReq{ID: id})
	wantErr(t, err, "")

	var count int64
	if err := e.db.Model(&adminmodel.DeptEntity{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("部门应被删除")
	}
}

func TestDeptTreeBuild(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	rootA := deptCreate(t, e, "根A", "RA_"+uniq(""))
	deptCreate(t, e, "子A1", "CA1_"+uniq(""), rootA)
	deptCreate(t, e, "根B", "RB_"+uniq(""))

	tree, err := e.svc.DeptTree(ctx)
	wantErr(t, err, "")
	if len(tree) != 2 {
		t.Fatalf("应有两个根: %d", len(tree))
	}
	for i := range tree {
		if tree[i].DeptName == "根A" && len(tree[i].Children) != 1 {
			t.Fatalf("根A 应有一个子部门")
		}
	}
}

func TestAncestorIDsParse(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	root := deptCreate(t, e, "根", "AR_"+uniq(""))
	child := deptCreate(t, e, "子", "AC_"+uniq(""), root)
	grand := deptCreate(t, e, "孙", "AG_"+uniq(""), child)

	ids, err := e.svc.AncestorIDs(ctx, grand)
	wantErr(t, err, "")
	if len(ids) != 2 || ids[0] != root || ids[1] != child {
		t.Fatalf("祖先链不符: %v", ids)
	}

	_, err = e.svc.AncestorIDs(ctx, 888888)
	wantErr(t, err, adminenums.ErrDeptNotFound)
}

func TestDeptUpdateNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{
		ID: 777777, DeptName: "x", DeptCode: "X_" + uniq(""), Status: 1,
	})
	wantErr(t, err, adminenums.ErrDeptNotFound)
}

func deptCreate(t *testing.T, e *env, name, code string, parentIDs ...uint64) uint64 {
	t.Helper()
	req := &admindto.DeptCreateReq{DeptName: name, DeptCode: code, Status: 1}
	if len(parentIDs) > 0 {
		req.ParentID = parentIDs[0]
	}
	err := e.svc.DeptCreate(context.Background(), req)
	wantErr(t, err, "")
	return deptIDByCode(t, e, code)
}

func deptCreateWithID(t *testing.T, e *env, id uint64, name, code string, parentIDs ...uint64) uint64 {
	t.Helper()
	parentID := uint64(0)
	if len(parentIDs) > 0 {
		parentID = parentIDs[0]
	}
	ancestors := "0"
	if parentID != 0 {
		var parent adminmodel.DeptEntity
		if err := e.db.First(&parent, parentID).Error; err != nil {
			t.Fatalf("查询父部门失败: %v", err)
		}
		ancestors = parent.Ancestors + "," + uint64Str(parent.ID)
	}
	dept := &adminmodel.DeptEntity{
		ID: id, ParentID: parentID, Ancestors: ancestors,
		DeptName: name, DeptCode: code, Status: adminmodel.DeptStatusEnabled,
	}
	if err := e.db.Create(dept).Error; err != nil {
		t.Fatalf("创建部门失败: %v", err)
	}
	return id
}

func deptIDByCode(t *testing.T, e *env, code string) uint64 {
	t.Helper()
	var dept adminmodel.DeptEntity
	if err := e.db.Where("dept_code = ?", code).First(&dept).Error; err != nil {
		t.Fatalf("查询部门失败: %v", err)
	}
	return dept.ID
}

func deptCode(t *testing.T, e *env, id uint64) string {
	t.Helper()
	var dept adminmodel.DeptEntity
	if err := e.db.First(&dept, id).Error; err != nil {
		t.Fatalf("查询部门失败: %v", err)
	}
	return dept.DeptCode
}

func uint64Str(v uint64) string {
	return strconv.FormatUint(v, 10)
}
