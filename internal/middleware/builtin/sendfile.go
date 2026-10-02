package builtin

// sendfile.go — 让静态面真正走内核零拷贝（sendfile）。
//
// 【为什么现在是坏的】
// Go 的传输加速链只有一条：
//
//	http.ServeContent → io.CopyN(w, content, n)         // src 被 *io.LimitedReader 包住
//	→ *io.LimitedReader 不实现 io.WriterTo              // 于是 src 这一支走不通
//	→ 只能走 dst.(io.ReaderFrom)                        // ← 条件①：dst 链上得有人实现它
//	→ *http.response.ReadFrom → w.conn.rwc.(io.ReaderFrom)（*net.TCPConn）
//	→ net.TCPConn.ReadFrom → internal/poll.sendFile
//	→ **剥开 *io.LimitedReader** → 内层必须是 *os.File   // ← 条件②：源不能被包装
//	→ syscall.Sendfile
//
// 两个条件当前**都不成立**：
//
//	① gin 的 responseWriter 嵌入的是 http.ResponseWriter **接口**，接口方法集只有
//	   Header/Write/WriteHeader，不含 ReadFrom → 不提升 → *gin.responseWriter 不满足
//	   io.ReaderFrom。gin v1.12.0 全库 grep ReadFrom 零命中 —— 这不是版本问题，
//	   升级 gin 修不了；我们自己的 gzip / storageCache 包装嵌入的是 gin.ResponseWriter
//	   接口，同样不通。
//	② gin.Dir(root,false) 的 OnlyFilesFS 对文件也包装（见 static_fs.go）。
//
// 【为什么这条链断了也没人发现】
// 它是**静默退化**：响应字节完全正确、状态码完全正确、所有测试都绿，
// 只是内核多了一次 read+write 的全量内存搬运。所以判据不能是「读代码觉得对」，
// 必须是传输层真的收到了 ReadFrom 调用 —— 见 sendfile_probe_test.go 的观测方式。
//
// 【本文件的职责】
// 只提供两个原语：沿 writer 链向下找到真正握有连接的零拷贝目标、
// 把一次写请求转交过去（不能转就降级为普通拷贝）。各层自己在**职责完成之后**
// 调用它 —— 尤其是 gzip 层必须在自己决定完「压不压缩」、转发完 WriteHeader 之后
// 才转交，否则会跳过它的延迟写头语义。

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// sfWriterOnly 只暴露 Write，用来在降级拷贝时阻断对 io.ReaderFrom 的再次命中。
//
// io.Copy 先看 src 的 WriterTo、再看 dst 的 ReaderFrom。把 dst 收窄成纯 Writer
// 是「自己的 ReadFrom 调 io.Copy 又回到自己的 ReadFrom」这条无限递归的标准解法。
type sfWriterOnly struct{ io.Writer }

// sfUnwrapper 由 writer 链上每一层实现，用来向下穿透到自己包裹的那一层。
type sfUnwrapper interface{ Unwrap() http.ResponseWriter }

// sfZeroCopyTarget 沿 Unwrap 链向下找第一个实现 io.ReaderFrom 的 writer。
//
// 为什么必须逐层穿透、不能直接拿最内层：链上每一层都可能是「必须经过的一段」
// （要转发 WriteHeader、要决定是否压缩）同时又是「不实现 ReadFrom 的一段」。
// 只有一层层问下去，才能找到 *http.response —— 真正握有 net.TCPConn 的那一个。
//
// 深度上限 8 是防御：Unwrap 万一成环，立即停手降级，不进入死循环。
func sfZeroCopyTarget(w http.ResponseWriter) io.ReaderFrom {
	for depth := 0; w != nil && depth < 8; depth++ {
		if rf, ok := w.(io.ReaderFrom); ok {
			return rf
		}
		u, ok := w.(sfUnwrapper)
		if !ok {
			return nil
		}
		w = u.Unwrap()
	}
	return nil
}

// sfForward 把一次写请求交给 inner：能零拷贝就零拷贝，否则降级为普通拷贝。
//
// 调用前提：调用方必须**已经完成自己的语义**（gzip 层已定压缩与否并转发 WriteHeader，
// cache 层已按状态码落缓存头）。它只管转发字节，不替任何一层做决定。
func sfForward(inner gin.ResponseWriter, r io.Reader) (int64, error) {
	if rf := sfZeroCopyTarget(inner); rf != nil {
		return rf.ReadFrom(r)
	}
	return io.Copy(sfWriterOnly{inner}, r)
}

// sfCommitHeader 把已经记录下来的状态码**真正写进连接**，让后续的 ReadFrom 不再
// 走标准库的嗅探分支。
//
// 【为什么必须做】
// 链上有三个不同的「写了 header 没有」标志，容易混为一谈：
//
//	gin.responseWriter.size      —— gin 自己记的 body 长度（-1 表示没写过）
//	(*http.response).wroteHeader —— 标准库记的「状态码定了」，WriteHeader 设它
//	(*chunkWriter).wroteHeader   —— 标准库记的「状态行+头**字节**出去了」
//
// gin 的 WriteHeader 只改第一个；(*response).WriteHeader 只改第二个（**不写字节**）。
// 而 (*http.response).ReadFrom 恰好用**第三个**决定是否要先做 Content-Type 嗅探：
//
//	if !w.cw.wroteHeader {
//	    io.CopyBuffer(writerOnly{w}, io.LimitReader(src, sniffLen), buf)   // sniffLen=512
//	    if err != nil || n0 < sniffLen { return n, err }                   // ← 不足就提前返回
//	}
//
// 只要第三个别志还是 false，**小于 512 字节的响应就永远走不到零拷贝那一段** ——
// 而访问面的 .gz 产物通常只有一两百字节，正好落在盲区里。
//
// 【为什么空写不行、必须 Flush】
// 试过「WriteHeaderNow + w.Write(nil)」，无效。因为 (*response).write 里
// `if lenData == 0 { return 0, nil }` 在碰 chunkWriter 之前就返回了；而真正写 body
// 走的又是 w.w（bufio，2048 缓冲），小响应根本冲不到 chunkWriter。
// 唯一能从外部触发 chunkWriter 落头的公开入口是 (*response).Flush ——
// 它内部的 cw.flush() 会 `if !cw.wroteHeader { cw.writeHeader(nil) }`。
//
// gin 的 Flush 恰好一步到位：先 WriteHeaderNow 把状态码交给标准库（定型），
// 再转调 (*response).Flush（落头）。提前 flush 不会改变响应语义：
// http.ServeContent 已经设了 Content-Length，chunkWriter 因此走非 chunked 路径。
func sfCommitHeader(w gin.ResponseWriter) {
	w.Flush()
}

// sendfileWriter 给任意 gin.ResponseWriter 补上 io.ReaderFrom。
//
// 【什么时候需要它】
// 中间件在 c.Next() 之前替换 c.Writer，**后执行的中间件才包在外面**。所以当一个
// 中间件要自己调用 http.ServeContent(c.Writer, …) 并当场结束请求时（例如
// PrecompressedAssetMiddleware，它排在 StaticGzip 之前），它看到的 c.Writer
// 还不包含后面那些层的包装 —— 而裸的 *gin.responseWriter 不实现 io.ReaderFrom。
// 这种情况下「在链尾再加一个中间件」救不了它，只能自己包一层。
//
// 与 sfForward 的分工：sfForward 是给**已经在链上的层**做转发用的（它们自己实现了
// ReadFrom）；sendfileWriter 是把**不在链上的一次性 writer** 变成有能力零拷贝的。
type sendfileWriter struct {
	gin.ResponseWriter

	// zc 在构造时解析一次。解析结果在整个响应生命周期内不会变（writer 链在
	// handler 执行期间是固定的），逐次写请求再去穿透一遍纯属浪费。
	zc io.ReaderFrom
}

var (
	_ gin.ResponseWriter = (*sendfileWriter)(nil)
	_ io.ReaderFrom      = (*sendfileWriter)(nil)
)

func newSendfileWriter(w gin.ResponseWriter) *sendfileWriter {
	return &sendfileWriter{ResponseWriter: w, zc: sfZeroCopyTarget(w)}
}

// Unwrap 保持穿透链完整：包在外层之后仍然能找到最里面握有连接的那一层。
func (w *sendfileWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ReadFrom 先让状态码真正落到连接上，再转交字节。
//
// sfCommitHeader 不是可选优化：不补它，小于 512 字节的响应会被标准库的嗅探分支
// 提前返回，零拷贝静默失效（详见 sfCommitHeader 注释）。
func (w *sendfileWriter) ReadFrom(r io.Reader) (int64, error) {
	sfCommitHeader(w.ResponseWriter)
	if w.zc != nil {
		return w.zc.ReadFrom(r)
	}
	return io.Copy(sfWriterOnly{w}, r)
}
