package llm

// maxSSELineBytes bounds a single server-sent-event line. bufio.Scanner's 64 KB
// default is well under what a provider emits for a large tool-call argument
// delta, and hitting it aborts the whole stream with "token too long".
const maxSSELineBytes = 8 * 1024 * 1024
