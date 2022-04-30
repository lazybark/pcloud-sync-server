package main

import (
	"github.com/lazybark/pcloud-sync-server/sync"
)

func main() {
	/*
		Сервер считает количество подключений для каждого юзера
		Он учитывает, что другая сторона запросила максимум N активных подключений и не будет слать лишний раз, точно так же, как и клиент

		Сервер




	*/

	// Как ускорить работу с массивами?

	//Mode в конфиге решает, в каком режиме будет работать сервер
	// SERVER - regular main server, MIRROR - server that just gets and stores current structure
	// ENDPOINT - server that does not store data - just passes on fs-events

	//File versioning

	//Рутина, которая чистит старый action buffer
	//To DO: -v flag for server verbose logging, -f for filesystem verbose logging
	//-s flag for total silent mode
	//-t for full user+token truncate at start
	//-b for backing up the DB
	//--stats
	//-c use colorful output in console (true by default)

	//Сервер отсылает обратно свой конфиг: макс число одновременных коннектов, макс размер файла
	//Макс соединений не может быть меньше 2 (одно для информации, другое для отправки/приема файлов)

	//TO DO: divide files and folders between users
	//TO DO: encode files on the fly
	// Single client mode - delete all tokens if new registered

	// Duplicate servers to avoid data loss
	// Data archiving and file versioning

	// Client sends DeviceName

	/// QR-код, чтобы поделиться файлом
	// Полноценная возможность делиться файлами из приватного хранилища

	// Полностью консольная версия облака

	// Отдельная настройка, чтобы логировать только в консоль / файл / и туда, и туда

	// Указать, что для любых интов 0 - это выключено

	// Как распространять сертификаты?

	// Сколько было активно соединение
	// Сколько трафика прогнал клиент
	// Подхват конфига на лету
	// Ограничние на максимальный размер файла
	// Ограничение трафика / кол-ва файлов и прочего
	//Требовать перелогин при каждой новой сессии

	// Exclude from sync

	// Возможность конфигурировать сервер как бекапный - только получает данные, ничего не отправляет. Тогда он и не следит за изменениями

	//Собирать ошибки в том числе

	// Что очищать после закрытия сервера?

	// Рутина, котоарая чистит устаревшие соединения

	server := sync.NewSyncServer()
	defer server.Watcher.Close()
	go server.InterruptCatcher()
	server.Start()

	//users.CreateUserCLI(sqlite)

}
