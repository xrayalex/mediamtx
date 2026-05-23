//
// Created by ilya on 23.09.2025.
//

#ifndef PLATECORE_TYPES_H
#define PLATECORE_TYPES_H
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif
    typedef void* platecore_obj;

    // Тип номера
    typedef enum {
        PLATE_TYPE_AUTO = 0,
        PLATE_TYPE_WAGON = 1,
        PLATE_TYPE_BOAT = 2,
        PLATE_TYPE_CONTAINER = 3
    } plate_type;

    // Формат входных данных
    typedef enum {
        YUV420P = 0,
        NV12 = 1,
        BGR = 2,
        RGB = 3,
      } pixel_format;

    typedef enum
    {
        MODE_ENTER_ONCE     = 0, // событие один раз при входе (после N кадров)
        MODE_ENTER_INTERVAL = 1, // события периодически, пока объект в зоне
        MODE_LEAVE          = 2,  // событие при завершении трека (объект потерян). Включает буфферизацию входа.
        MODE_RAW = 3, // События с отключенным частотником номеров ,  работает только параметр min_hits, repeat_event и ttl - отключаются
    } mode_filter;

    typedef struct {
        int is_valid;
        int tracker_id;
        char* plate_text_full;
        char* plate_text;
        char* region;
        char* country;
        float score;
        int bbox[4];
        // Other info
        int direction_left_right; // 0 - left, 1 - right , -1  - stationary
        int direction_up_down; // 0 - up, 1 - down , -1  - stationary
        int layout; // 0 - rectangle, 1-square
        float speed; // средняя скорость объекта в км/ч (0 — недостоверно/недостаточно данных)
        //Frame info
        uint64_t timestamp;
        void* frame;
        int width;
        int height;
        pixel_format format;

    } processing_result;

    typedef struct {
        //MAIN PARAM
        float roi_rect[4] = {0.0f,0.0f,0.0f,0.0f}; // Регион детектирования номеров в нормализированом виде [0.0,1.0]. Если не используется [0.0,0.0,0.0,0.0]. Формат xmin,ymin,xmax,ymax.
        float plate_size_min[2] = {0.0f,0.0f}; // Минимальный размер номера  в нормализированом виде [0.0,1.0]. Если не используется [0.0,0.0]. Формат w,h.
        float plate_size_max[2] = {0.0f,0.0f}; // Максимальный размер номера в нормализированом виде [0.0,1.0]. Если не используется [0.0,0.0]. Формат w,h.
        //RECOGNIZE PARAM
        int min_hits = 3; // кол-во номеров для лучшего распознования. 0 - до потери трека аккумулирует лучшее, >0 - выдать лучший из кол-ва оптимальное(3).
        //EVENT PARAM
        mode_filter mode = MODE_LEAVE; // 0 - пока трекер живой или номер будет одинаковый будет передавать только одно событие, 1 - будет отправлять не чаще repeat_event
        int repeat_event=5; // Время паузы для отправки. В течении паузы событие не будет приходить, а будет отбрасываться.
        int ttl = 60; // Время жизни номера, после сброса если номер будет в кадре , создаст событие.
        //STREAM PARAM
        int stream = 1; // 0 - Режим кадра , в данном режиме отключается трекер и (min_hits=1 и mode=0) для работы с изображением. 1 - Режим потока.
        //DEVICE PARAM
        int gpu = 0; // -1 — CPU, 0..N — индекс GPU для CUDA (только PLATFORM_X86). На PLATFORM_RK игнорируется.


    } plate_core_init_arg;

    typedef struct {
      void* buffer;
      int size;
    } jpeg_buffer;

    typedef int32_t plate_core_retcode;

    enum PlateCoreStatus {
        PLATECORE_OK             = 0,
        PLATECORE_ERR_INVALID_ARG = -1,
        PLATECORE_ERR_INDEX_OUT_RANGE = -2,
        PLATECORE_ERR_NO_MEMORY   = -2,
        PLATECORE_ERR_INTERNAL    = -3,
        PLATECORE_ERR_INTERNAL_SERVER = -500,
        PLATECORE_ERR_NOT_FOUND_LICENSE = -501,
        PLATECORE_ERR_LICENSE_HARDWARE_MISMATCH = -502,
        PLATECORE_ERR_LICENSE_BUSY = -503,
        PLATECORE_ERR_NOT_FOUND = -504,
    } ;





#ifdef __cplusplus
}
#endif
#endif //PLATECORE_TYPES_H
