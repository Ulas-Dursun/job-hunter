package analyzer

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxErrBodyBytes, sağlayıcı hata gövdesinden log/hata mesajına taşınacak azami bayt sayısıdır.
const maxErrBodyBytes = 512

// httpStatusError, AI sağlayıcısından dönen 200 dışı HTTP yanıtını tipli olarak taşır.
// Böylece 429/503 tespiti hata metnindeki rastgele "429" geçişlerine bağlı kalmaz.
type httpStatusError struct {
	Provider string
	Status   int
	Body     string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s status %d: %s", e.Provider, e.Status, e.Body)
}

// newStatusError yanıt gövdesini sınırlı okuyup tipli hata üretir.
func newStatusError(provider string, resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
	return &httpStatusError{Provider: provider, Status: resp.StatusCode, Body: string(b)}
}

// ErrAIBusy, tüm AI istasyonları geçici olarak (429/503) yanıt vermediğinde döner.
// Çağıran taraf ilanı kalıcı hataya taşımak yerine daha sonra yeniden denemelidir.
var ErrAIBusy = errors.New("all AI endpoints busy")

// IsTransient, hatanın geçici (kota/yoğunluk) olup olmadığını söyler.
func IsTransient(err error) bool {
	return errors.Is(err, ErrAIBusy) || isRetryable(err)
}

// isRetryable, hatanın geçici (429/503) olup olmadığını söyler.
func isRetryable(err error) bool {
	var se *httpStatusError
	if errors.As(err, &se) {
		return se.Status == http.StatusTooManyRequests || se.Status == http.StatusServiceUnavailable
	}
	return false
}
