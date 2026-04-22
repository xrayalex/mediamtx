//
// Created by ilya on 23.09.2025.
//

#ifndef PLATE_CORE_H
#define PLATE_CORE_H
#include "platecore_types.h"
#define PLATECORE_API __attribute__((visibility("default")))

#ifdef __cplusplus
extern "C" {
#endif

    // Получить версию библиотеки
   PLATECORE_API const char* get_version();
    // Получить указатель на объект platecore_obj;
   PLATECORE_API platecore_obj create_platecore_obj();
    // Инициализация объекта с текущими параметрами
   PLATECORE_API plate_core_retcode platecore_init(platecore_obj* obj ,plate_core_init_arg* arg, plate_type type);
    // Обновление настроек
   PLATECORE_API plate_core_retcode platecore_update(platecore_obj* obj, plate_core_init_arg* arg);
    // Отправление кадра
   PLATECORE_API plate_core_retcode platecore_proccesing(platecore_obj* obj,void* data, int width, int  height,int timestamp, pixel_format format = YUV420P);
    // получить количество событий в последнем кадре
   PLATECORE_API int platecore_get_result_count(platecore_obj* obj);
    // получить событие по индексу
   PLATECORE_API plate_core_retcode platecore_get_result_by_index(platecore_obj* obj, int index, processing_result* out_event);
    // Получение JPEG byte, есле crop 0 то возвращает буффер jpeg полного кадра, если 1 - то обрезает по bbox и возвращает jpeg буффер.
   PLATECORE_API plate_core_retcode platecore_to_jpeg(processing_result* out_event,jpeg_buffer* buffer, int crop,int draw);

    // освобождение памяти
   PLATECORE_API plate_core_retcode platecore_processing_release(platecore_obj* obj,processing_result* out_event);

    // освобождение памяти JPEG
   PLATECORE_API plate_core_retcode platecore_jpeg_release(jpeg_buffer* buffer);

    // уничтожить объект
   PLATECORE_API plate_core_retcode platecore_release(platecore_obj* obj);





#ifdef __cplusplus
}
#endif
#endif //PLATE_CORE_H
