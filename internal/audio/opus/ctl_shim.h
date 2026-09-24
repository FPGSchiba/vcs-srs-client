#ifndef VCS_OPUS_CTL_SHIM_H
#define VCS_OPUS_CTL_SHIM_H

#include <opus.h>

/* cgo cannot call variadic C functions, and opus_encoder_ctl() is variadic.
   This non-variadic wrapper is the only way to reach the CTL interface from
   Go. It covers exactly the setter shape we need: one opus_int32 argument. */
int opus_encoder_ctl_set(OpusEncoder *st, int request, opus_int32 value);

#endif /* VCS_OPUS_CTL_SHIM_H */
