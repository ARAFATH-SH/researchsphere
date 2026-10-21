package httpx

// MediaURL is where the client reads a stored file: GET /media/{fileId} checks that the
// caller may see it and redirects to a presigned URL (NFR-13). Responses carry this path,
// never a presigned URL, because those stop working after minutes.
func MediaURL(fileID string) string {
	return "/api/v1/media/" + fileID
}
