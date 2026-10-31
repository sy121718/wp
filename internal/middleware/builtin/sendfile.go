package builtin

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

type sfWriterOnly struct{ io.Writer }

type sfUnwrapper interface{ Unwrap() http.ResponseWriter }

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

func sfForward(inner gin.ResponseWriter, r io.Reader) (int64, error) {
	if rf := sfZeroCopyTarget(inner); rf != nil {
		return rf.ReadFrom(r)
	}
	return io.Copy(sfWriterOnly{inner}, r)
}

func sfCommitHeader(w gin.ResponseWriter) {
	w.Flush()
}

type sendfileWriter struct {
	gin.ResponseWriter

	zc io.ReaderFrom
}

var (
	_ gin.ResponseWriter = (*sendfileWriter)(nil)
	_ io.ReaderFrom      = (*sendfileWriter)(nil)
)

func newSendfileWriter(w gin.ResponseWriter) *sendfileWriter {
	return &sendfileWriter{ResponseWriter: w, zc: sfZeroCopyTarget(w)}
}

func (w *sendfileWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *sendfileWriter) ReadFrom(r io.Reader) (int64, error) {
	sfCommitHeader(w.ResponseWriter)
	if w.zc != nil {
		return w.zc.ReadFrom(r)
	}
	return io.Copy(sfWriterOnly{w}, r)
}
