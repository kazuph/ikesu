#!/usr/bin/env python3
"""Hash decision contracts, without reading or writing a real clipboard."""
import hashlib
import unittest

from clipboard_guard import require_no_copy_write


def record(before, after, copied):
    digest = lambda value: hashlib.sha256(value).hexdigest()
    return {'before_sha256': digest(before), 'after_sha256': digest(after),
            'copy_candidate_sha256': [digest(value) for value in copied]}


class ClipboardPolicy(unittest.TestCase):
    def test_external_changes_continue_even_when_repeated(self):
        require_no_copy_write([record(b'initial', b'external-one', [b'copy']),
                               record(b'initial', b'external-two', [b'copy'])])

    def test_changed_hash_matching_any_product_copy_stops(self):
        records = [record(b'initial', b'other-product-copy', []),
                   record(b'initial', b'external', [b'other-product-copy'])]
        with self.assertRaisesRegex(RuntimeError, 'STOP:'):
            require_no_copy_write(records)

    def test_changed_hash_matching_copy_with_newline_stops(self):
        with self.assertRaisesRegex(RuntimeError, 'STOP:'):
            require_no_copy_write([record(b'initial', b'copy\n', [b'copy', b'copy\n'])])

    def test_unchanged_hash_does_not_attribute_existing_content_to_copy(self):
        require_no_copy_write([record(b'copy', b'copy', [b'copy'])])


if __name__ == '__main__':
    unittest.main()
