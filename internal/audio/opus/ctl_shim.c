#include <opus.h>

#include "ctl_shim.h"

int opus_encoder_ctl_set(OpusEncoder *st, int request, opus_int32 value) {
    return opus_encoder_ctl(st, request, value);
}

int opus_encoder_ctl_get(OpusEncoder *st, int request, opus_int32 *value) {
    return opus_encoder_ctl(st, request, value);
}
