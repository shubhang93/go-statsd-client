package statsd

// Returns a new TCPClient, and an error
//
// addr is a string of the format "hostname:port", and must be parsable by
// net.ResolveTCPAddr.
//
// prefix is the statsd client prefix. Can be "" if no prefix is desired.
//
// disableNagle specifies whether to disable Nagle's algorithm.
func NewTCPClient(addr, prefix string, disableNagle bool) (Statter, error) {
	sender, err := NewTCPSender(addr, disableNagle)
	if err != nil {
		return nil, err
	}

	client := &Client{
		prefix: prefix,
		sender: sender,
	}

	return client, nil
}
