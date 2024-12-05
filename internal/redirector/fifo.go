package redirector

import "os"

func clearFifo(path string) error {
	return os.Truncate(path, 0)
}

func readFifo(file *os.File) ([]int16, error) {
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil {
		return nil, err
	}

	samples := make([]int16, n/2)
	for i := 0; i < n; i += 2 {
		samples[i/2] = int16(buf[i]) | int16(buf[i+1])<<8
	}

	return samples, nil
}
