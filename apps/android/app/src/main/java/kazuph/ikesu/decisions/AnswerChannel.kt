package kazuph.ikesu.decisions

/** An answer can be supplied by another front end without coupling it to WebSocket. */
interface AnswerChannel {
    fun answer(decisionId: String, optionIds: List<String>, text: String, images: List<ByteArray>, sequence: String): Boolean
}
