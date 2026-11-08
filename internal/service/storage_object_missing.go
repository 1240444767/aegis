package service

import (
	"errors"
	"net/http"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/minio/minio-go/v7"
	cos "github.com/tencentyun/cos-go-sdk-v5"

	apperrors "aegis/pkg/errors"
)

// ErrStorageObjectMissing 存储提供商明确答复「对象不存在」。
//
// 与网络、权限、熔断这类故障分开：后者稍后重试就好，前者重试多少次都一样 ——
// 调用方要据此决定是报「稍后重试」还是「内容已经没了」。
var ErrStorageObjectMissing = errors.New("storage object missing")

// storageObjectMissingError 对外仍是原来的 40481 AppError（错误响应不变），
// 同时能被 errors.Is(err, ErrStorageObjectMissing) 认出来。
type storageObjectMissingError struct {
	*apperrors.AppError
}

func (e storageObjectMissingError) Unwrap() []error {
	return []error{e.AppError, ErrStorageObjectMissing}
}

// isStorageObjectNotFound 判断提供商 Open 的错误是不是「对象不存在」。
// 认不出来的一律当成故障：把一次网络抖动误判成「内容丢了」比反过来糟得多。
func isStorageObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	// 本地存储、WebDAV、OneDrive、Dropbox、Google Drive 找不到时回的是 404 AppError。
	if appErr, ok := errors.AsType[*apperrors.AppError](err); ok {
		return appErr.HTTPStatus == http.StatusNotFound
	}
	if code := minio.ToErrorResponse(err).Code; code == "NoSuchKey" {
		return true
	}
	// aws-sdk-go-v2（S3 / R2）：GetObject 回 NoSuchKey，HeadObject 没有响应体只有 404（NotFound）。
	var noSuchKey *s3types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
		return true
	}
	var responseErr *awshttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	if ossErr, ok := errors.AsType[oss.ServiceError](err); ok {
		return ossErr.Code == "NoSuchKey"
	}
	if cos.IsNotFoundError(err) {
		return true
	}
	return bloberror.HasCode(err, bloberror.BlobNotFound)
}
