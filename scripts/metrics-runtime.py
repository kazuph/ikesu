#!/usr/bin/env python3
"""Runtime measurement helpers for the isolated mini benchmark."""

import json
import math
import os
import statistics
from pathlib import Path


def latency_summary(samples):
    """Nearest-rank percentiles; an unsuccessful observation invalidates them."""
    observed = [s['seconds'] for s in samples if s['status'] == 'ok']
    failed = len(samples) - len(observed)
    result = {'sent': len(samples), 'observed': len(observed), 'failed': failed,
              'percentile_method': 'nearest-rank'}
    if not failed and observed:
        ordered = sorted(observed)
        result.update(p50_seconds=ordered[math.ceil(0.50 * len(ordered)) - 1],
                      p99_seconds=ordered[math.ceil(0.99 * len(ordered)) - 1])
    return result


def median_three(samples):
    if len(samples) != 3 or any(s['status'] != 'ok' for s in samples):
        return None
    return statistics.median(s['value'] for s in samples)


def save_json(path, data):
    """Replace a single run atomically so interruption cannot leave partial JSON."""
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + '.tmp-' + str(os.getpid()))
    try:
        with temporary.open('x', encoding='utf-8') as stream:
            json.dump(data, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)
