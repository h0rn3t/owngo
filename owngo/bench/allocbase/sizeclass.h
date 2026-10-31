#ifndef OWNGO_SIZECLASS_H
#define OWNGO_SIZECLASS_H

#include <stddef.h>

void sc_init(void);
void *sc_malloc(size_t size);
void sc_free_block(void *p);
void *sc_base(const void *p);

#endif
