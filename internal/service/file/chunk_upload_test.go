package file

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// putChunk 是分片上传的测试辅助：按会话切片并投递第 index 片。
func putChunk(t *testing.T, svc *Service, uploadID string, index int, data []byte, checksum string) *model.FileUploadChunkResponse {
	t.Helper()
	res, err := svc.UploadChunk(ChunkRequest{
		UploadID: uploadID,
		Index:    index,
		Size:     int64(len(data)),
		Checksum: checksum,
		Src:      bytes.NewReader(data),
	})
	if err != nil {
		t.Fatalf("投递分片 %d：%v", index, err)
	}
	return res
}

func TestChunkUploadMergesInOrder(t *testing.T) {
	svc, work := newTestService(t)
	// 3 片：两整片加一个 16 字节的末片
	payload := bytes.Repeat([]byte("ab"), (minChunkSize*2+16)/2)

	init, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "app.bin", Size: int64(len(payload)), ChunkSize: 256,
	})
	if err != nil {
		t.Fatalf("init：%v", err)
	}
	// 过小的 chunkSize 会被抬到下限，避免分片数爆炸
	if init.ChunkSize != minChunkSize {
		t.Fatalf("chunkSize 应被抬到下限 %d，得到 %d", minChunkSize, init.ChunkSize)
	}
	if init.TotalChunks != 3 || len(init.UploadedChunks) != 0 {
		t.Fatalf("首次 init 结果不符：%+v", init)
	}

	chunkSize := int(init.ChunkSize)
	for i := 0; i < init.TotalChunks; i++ {
		end := min((i+1)*chunkSize, len(payload))
		res := putChunk(t, svc, init.UploadID, i, payload[i*chunkSize:end], "")
		if res.UploadedCount != i+1 || res.TotalChunks != init.TotalChunks {
			t.Fatalf("分片 %d 回执不符：%+v", i, res)
		}
	}

	res, err := svc.CompleteUpload(model.FileUploadCompleteRequest{UploadID: init.UploadID})
	if err != nil {
		t.Fatalf("complete：%v", err)
	}
	target := filepath.Join(work, "app.bin")
	if res.Path != target || res.Size != int64(len(payload)) {
		t.Fatalf("合并结果不符：%+v", res)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读合并结果：%v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("合并后内容与原始载荷不一致")
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat：%v", err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("落地权限应为 0644，得到 %v", fi.Mode().Perm())
	}
	// 会话目录应被清理
	if _, err := os.Stat(filepath.Join(svc.cfg.UploadTempDir, init.UploadID)); !os.IsNotExist(err) {
		t.Fatal("合并成功后会话目录应被删除")
	}
}

func TestChunkUploadResumesAndReportsMissing(t *testing.T) {
	svc, work := newTestService(t)
	payload := bytes.Repeat([]byte("z"), minChunkSize*2+16) // 3 片，末片 16 字节

	init, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "big.bin", Size: int64(len(payload)), ChunkSize: minChunkSize,
	})
	if err != nil {
		t.Fatalf("init：%v", err)
	}
	if init.TotalChunks != 3 {
		t.Fatalf("应切成 3 片，得到 %d", init.TotalChunks)
	}
	chunkSize := int(init.ChunkSize)

	// 只传第 0 与第 2 片，第 1 片故意缺失
	putChunk(t, svc, init.UploadID, 0, payload[:chunkSize], "")
	putChunk(t, svc, init.UploadID, 2, payload[2*chunkSize:], "")

	status, err := svc.UploadStatus(init.UploadID)
	if err != nil {
		t.Fatalf("status：%v", err)
	}
	if fmt.Sprint(status.UploadedChunks) != "[0 2]" || fmt.Sprint(status.MissingChunks) != "[1]" {
		t.Fatalf("续传进度不符：uploaded=%v missing=%v", status.UploadedChunks, status.MissingChunks)
	}

	// 缺片时 complete 必须按 400006 拒绝，并给出待重传序号
	_, err = svc.CompleteUpload(model.FileUploadCompleteRequest{UploadID: init.UploadID})
	if errs.Code(err) != errs.CodeChunkChecksum {
		t.Fatalf("缺片应返回 %d，得到 %v", errs.CodeChunkChecksum, err)
	}
	var missing *MissingChunksError
	if !errors.As(err, &missing) || fmt.Sprint(missing.Missing) != "[1]" {
		t.Fatalf("缺片错误未带上 missing：%v", err)
	}

	// 同参数重新 init 应复用会话，让前端接着传缺的那片
	again, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "big.bin", Size: int64(len(payload)), ChunkSize: minChunkSize,
	})
	if err != nil {
		t.Fatalf("重新 init：%v", err)
	}
	if again.UploadID != init.UploadID || fmt.Sprint(again.UploadedChunks) != "[0 2]" {
		t.Fatalf("同参数 init 应复用会话：%+v", again)
	}

	putChunk(t, svc, init.UploadID, 1, payload[chunkSize:2*chunkSize], "")
	if _, err := svc.CompleteUpload(model.FileUploadCompleteRequest{UploadID: init.UploadID}); err != nil {
		t.Fatalf("补齐后 complete：%v", err)
	}
	got, err := os.ReadFile(filepath.Join(work, "big.bin"))
	if err != nil {
		t.Fatalf("读结果：%v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("断点续传后的内容不一致")
	}
}

func TestChunkUploadValidatesChunk(t *testing.T) {
	svc, work := newTestService(t)
	payload := bytes.Repeat([]byte("q"), minChunkSize+8)

	init, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "v.bin", Size: int64(len(payload)), ChunkSize: minChunkSize,
	})
	if err != nil {
		t.Fatalf("init：%v", err)
	}
	chunkSize := int(init.ChunkSize)
	first := payload[:chunkSize]

	// 校验和不匹配
	bad := md5.Sum([]byte("nope"))
	_, err = svc.UploadChunk(ChunkRequest{
		UploadID: init.UploadID, Index: 0, Size: int64(len(first)),
		Checksum: "md5:" + hex.EncodeToString(bad[:]), Src: bytes.NewReader(first),
	})
	if errs.Code(err) != errs.CodeChunkChecksum {
		t.Fatalf("校验和不符应返回 %d，得到 %v", errs.CodeChunkChecksum, err)
	}
	if _, statErr := os.Stat(partPath(filepath.Join(svc.cfg.UploadTempDir, init.UploadID), 0)); !os.IsNotExist(statErr) {
		t.Fatal("校验失败的分片不得留在会话目录")
	}

	// 长度与期望不符（非末片短传）
	_, err = svc.UploadChunk(ChunkRequest{
		UploadID: init.UploadID, Index: 0, Src: bytes.NewReader(first[:10]),
	})
	if errs.Code(err) != errs.CodeChunkChecksum {
		t.Fatalf("短传应返回 %d，得到 %v", errs.CodeChunkChecksum, err)
	}

	// 序号越界
	_, err = svc.UploadChunk(ChunkRequest{UploadID: init.UploadID, Index: 9, Src: bytes.NewReader(nil)})
	if errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("越界序号应返回 %d，得到 %v", errs.CodeInvalidParam, err)
	}

	// 正确的 md5 校验和可以通过
	sum := md5.Sum(first)
	putChunk(t, svc, init.UploadID, 0, first, "md5:"+hex.EncodeToString(sum[:]))

	// sha256 校验和同样支持
	last := payload[chunkSize:]
	s := sha256.Sum256(last)
	putChunk(t, svc, init.UploadID, 1, last, "sha256:"+hex.EncodeToString(s[:]))

	if _, err := svc.CompleteUpload(model.FileUploadCompleteRequest{UploadID: init.UploadID}); err != nil {
		t.Fatalf("complete：%v", err)
	}
}

func TestChunkUploadWholeFileHash(t *testing.T) {
	svc, work := newTestService(t)
	payload := []byte("novapanel-chunked-upload")
	sum := sha256.Sum256(payload)
	hash := "sha256:" + hex.EncodeToString(sum[:])

	init, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "h.bin", Size: int64(len(payload)), Hash: hash,
	})
	if err != nil {
		t.Fatalf("init：%v", err)
	}
	if init.QuickUpload {
		t.Fatal("目标不存在时不应命中秒传")
	}
	putChunk(t, svc, init.UploadID, 0, payload, "")

	// 声明的整文件哈希与实际不符：合并必须失败且不落地
	_, err = svc.CompleteUpload(model.FileUploadCompleteRequest{
		UploadID: init.UploadID, Hash: "sha256:" + strings.Repeat("0", 64),
	})
	if errs.Code(err) != errs.CodeChunkChecksum {
		t.Fatalf("整文件哈希不符应返回 %d，得到 %v", errs.CodeChunkChecksum, err)
	}
	if _, statErr := os.Stat(filepath.Join(work, "h.bin")); !os.IsNotExist(statErr) {
		t.Fatal("哈希校验失败不得留下目标文件")
	}

	// 合并失败后会话仍可用，重试（哈希正确）应成功
	res, err := svc.CompleteUpload(model.FileUploadCompleteRequest{UploadID: init.UploadID, Hash: hash})
	if err != nil {
		t.Fatalf("重试 complete：%v", err)
	}
	if res.Hash != hash {
		t.Fatalf("回显哈希不符：%s", res.Hash)
	}

	// 同名同哈希再次 init 命中秒传
	quick, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "h.bin", Size: int64(len(payload)), Hash: hash,
	})
	if err != nil {
		t.Fatalf("秒传 init：%v", err)
	}
	if !quick.QuickUpload || quick.Entry == nil || quick.Entry.Size != int64(len(payload)) {
		t.Fatalf("应命中秒传：%+v", quick)
	}
}

func TestChunkUploadSessionLifecycle(t *testing.T) {
	svc, work := newTestService(t)

	// 伪造 uploadId 必须在落盘前被拒，杜绝借会话目录做路径穿越
	for _, bad := range []string{"", "up_../../etc", "up_zz", strings.Repeat("a", 80)} {
		if _, err := svc.UploadStatus(bad); errs.Code(err) != errs.CodeInvalidParam {
			t.Fatalf("uploadId %q 应被拒，得到 %v", bad, err)
		}
	}
	// 格式合法但不存在的会话按 404 处理
	if _, err := svc.UploadStatus("up_" + strings.Repeat("ab", 12)); errs.Code(err) != errs.CodeFileNotFound {
		t.Fatalf("不存在的会话应返回 %d", errs.CodeFileNotFound)
	}

	init, err := svc.InitUpload(model.FileUploadInitRequest{Path: work, Filename: "t.bin", Size: 8})
	if err != nil {
		t.Fatalf("init：%v", err)
	}
	sessionDir := filepath.Join(svc.cfg.UploadTempDir, init.UploadID)

	// 把创建时间改到 TTL 之前，模拟过期会话
	meta, err := readMeta(sessionDir)
	if err != nil {
		t.Fatalf("读 meta：%v", err)
	}
	meta.CreatedAt = time.Now().Add(-uploadTTL - time.Hour).UnixMilli()
	if err := writeMeta(sessionDir, meta); err != nil {
		t.Fatalf("回写 meta：%v", err)
	}
	if _, err := svc.UploadStatus(init.UploadID); errs.Code(err) != errs.CodeUploadExpired {
		t.Fatalf("过期会话应返回 %d，得到 %v", errs.CodeUploadExpired, err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatal("过期会话应被顺手清理")
	}

	// abort 幂等：会话不存在也返回成功
	again, err := svc.InitUpload(model.FileUploadInitRequest{Path: work, Filename: "t.bin", Size: 8})
	if err != nil {
		t.Fatalf("重新 init：%v", err)
	}
	if err := svc.AbortUpload(again.UploadID); err != nil {
		t.Fatalf("abort：%v", err)
	}
	if err := svc.AbortUpload(again.UploadID); err != nil {
		t.Fatalf("重复 abort 应幂等：%v", err)
	}
	if _, err := os.Stat(filepath.Join(svc.cfg.UploadTempDir, again.UploadID)); !os.IsNotExist(err) {
		t.Fatal("abort 后会话目录应被删除")
	}
}

func TestChunkUploadInitGuards(t *testing.T) {
	svc, work := newTestService(t)
	svc.cfg.MaxUploadSize = 1 << 20

	// 白名单外目录
	if _, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: filepath.Dir(work), Filename: "x.bin", Size: 1,
	}); errs.Code(err) != errs.CodePathEscape {
		t.Fatalf("白名单外应返回 %d，得到 %v", errs.CodePathEscape, err)
	}

	// 超过上传上限
	if _, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "x.bin", Size: svc.cfg.MaxUploadSize + 1,
	}); errs.Code(err) != errs.CodeFileTooLarge {
		t.Fatalf("超限应返回 %d，得到 %v", errs.CodeFileTooLarge, err)
	}

	// 文件名带路径分隔符时只取末段，不允许跳出目标目录
	init, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "../escape.bin", Size: 4,
	})
	if err != nil {
		t.Fatalf("init：%v", err)
	}
	putChunk(t, svc, init.UploadID, 0, []byte("data"), "")
	res, err := svc.CompleteUpload(model.FileUploadCompleteRequest{UploadID: init.UploadID})
	if err != nil {
		t.Fatalf("complete：%v", err)
	}
	if res.Path != filepath.Join(work, "escape.bin") {
		t.Fatalf("文件名应被规整到目标目录内：%s", res.Path)
	}

	// 分片数超限（先放开总大小上限，否则会先被 400005 拦下）
	svc.cfg.MaxUploadSize = 2 << 30
	if _, err := svc.InitUpload(model.FileUploadInitRequest{
		Path: work, Filename: "many.bin", Size: int64(maxChunks+1) * minChunkSize, ChunkSize: minChunkSize,
	}); errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("分片数超限应返回 %d，得到 %v", errs.CodeInvalidParam, err)
	}
}

func TestChunkUploadConflictPolicies(t *testing.T) {
	svc, work := newTestService(t)
	mustWrite(t, filepath.Join(work, "c.txt"), "old")

	upload := func(conflict string) (*model.FileUploadCompleteResponse, error) {
		init, err := svc.InitUpload(model.FileUploadInitRequest{
			Path: work, Filename: "c.txt", Size: 3, Conflict: conflict,
		})
		if err != nil {
			return nil, err
		}
		putChunk(t, svc, init.UploadID, 0, []byte("new"), "")
		return svc.CompleteUpload(model.FileUploadCompleteRequest{UploadID: init.UploadID})
	}

	if _, err := upload(model.FileConflictReject); errs.Code(err) != errs.CodeFileExists {
		t.Fatalf("默认应拒绝同名，得到 %v", err)
	}
	if _, err := upload(model.FileConflictOverwrite); err != nil {
		t.Fatalf("overwrite：%v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(work, "c.txt")); string(data) != "new" {
		t.Fatalf("overwrite 后内容应被替换，得到 %q", data)
	}
	res, err := upload(model.FileConflictRename)
	if err != nil {
		t.Fatalf("rename：%v", err)
	}
	if res.Path == filepath.Join(work, "c.txt") {
		t.Fatalf("rename 策略应另起文件名，得到 %s", res.Path)
	}
}
