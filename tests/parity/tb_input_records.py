"""Classify actual tb input; retain every unrecognized control sequence."""
import re


def typed_input(tapped):
    typed = bytearray()
    unknown = []
    cursor = 0
    while cursor < len(tapped):
        if tapped[cursor] != 27:
            typed.append(tapped[cursor])
            cursor += 1
            continue
        control = re.match(rb'\x1b([_P\]])(.*?)(?:\x1b\\|\x07)', tapped[cursor:], re.S)
        if control:
            if ((control[1] == b'_' and control[2].startswith(b'G'))
                    or (control[1] == b']' and re.match(rb'(?:4|10|11|12);', control[2]))
                    or (control[1] == b'P' and re.match(rb'(?:[01]\+r|[01]\$r)', control[2]))):
                cursor += len(control[0])
                continue
            unknown.append(control[0].hex())
            cursor += len(control[0])
            continue
        match = re.match(rb'\x1b\[[0-?]*[ -/]*[@-~]', tapped[cursor:])
        if match is None:
            unknown.append(tapped[cursor:].hex())
            break
        sequence = match[0]
        if re.fullmatch(rb'\x1b\[<\d+;\d+;\d+[Mm]', sequence):
            pass
        elif sequence in (b'\x1b[I', b'\x1b[O') or re.fullmatch(rb'\x1b\[\?\d+(?:;\d+)?(?:\$y|u)', sequence):
            pass
        elif sequence in (b'\x1b[200~', b'\x1b[201~'):
            pass
        elif key := re.fullmatch(rb'\x1b\[(\d+)(?:;\d+)?u', sequence):
            typed.extend(chr(int(key[1])).encode())
        elif not re.fullmatch(rb'\x1b\[[\d;?]*[Rct]', sequence):
            unknown.append(sequence.hex())
        cursor += len(sequence)
    return bytes(typed), unknown
