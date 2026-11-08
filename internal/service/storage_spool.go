package service

import (
	"fmt"
	"io"
	"os"
)

// rewindableContent 让上传内容可以从头再读一遍：存储池换成员重试时要把同一份内容再传一次。
//
// 本身能 Seek 的（multipart 文件、bytes.Reader、os.File）只记下起点；
// 不能 Seek 的流落到临时文件 —— 不读进内存，几百 MB 的安装包也不会把进程撑爆。
type rewindableContent struct {
	reader  io.ReadSeeker
	start   int64
	size    int64
	cleanup func()
}

func newRewindableContent(content io.Reader, declared int64) (*rewindableContent, error) {
	if content == nil {
		return nil, fmt.Errorf("上传内容为空")
	}
	if seeker, ok := content.(io.ReadSeeker); ok {
		start, err := seeker.Seek(0, io.SeekCurrent)
		if err == nil {
			size := declared
			if size <= 0 {
				end, endErr := seeker.Seek(0, io.SeekEnd)
				if endErr != nil {
					return nil, endErr
				}
				size = end - start
			}
			if _, err := seeker.Seek(start, io.SeekStart); err != nil {
				return nil, err
			}
			return &rewindableContent{reader: seeker, start: start, size: size, cleanup: func() {}}, nil
		}
	}
	file, err := os.CreateTemp("", "aegis-upload-*")
	if err != nil {
		return nil, err
	}
	remove := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	size, err := io.Copy(file, content)
	if err != nil {
		remove()
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		remove()
		return nil, err
	}
	return &rewindableContent{reader: file, size: size, cleanup: remove}, nil
}

// Rewind 回到内容开头。
func (r *rewindableContent) Rewind() error {
	_, err := r.reader.Seek(r.start, io.SeekStart)
	return err
}
